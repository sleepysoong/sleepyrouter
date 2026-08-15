// CLI - mirrors internal/cli/
import {
  ConfigStore,
  DEFAULT_PORT,
  resolveProviderAPIKeys,
  requireAnyProviderAPIKey,
} from "./config.js";
import {
  createSleepyRouterServer,
  listenServer,
} from "./server.js";
import { allGroupModelIDs } from "./routing.js";
import { getConfigPath, getEnvPath } from "./utils.js";
import type { ServerLogEvent } from "./handler.js";

const VERSION = "0.0.4";

function parseArgs(argv: string[]): {
  command: string;
  flags: Record<string, string | boolean>;
} {
  if (argv.length === 0) return { command: "help", flags: {} };
  const command = argv[0]!;
  const flags: Record<string, string | boolean> = {};
  for (let i = 1; i < argv.length; i++) {
    const arg = argv[i]!;
    if (arg.startsWith("--")) {
      const rest = arg.slice(2);
      const eqIdx = rest.indexOf("=");
      if (eqIdx >= 0) {
        flags[rest.slice(0, eqIdx)] = rest.slice(eqIdx + 1);
      } else if (i + 1 < argv.length && !argv[i + 1]!.startsWith("-")) {
        i++;
        flags[rest] = argv[i]!;
      } else {
        flags[rest] = true;
      }
    }
  }
  return { command, flags };
}

function parsePort(value: string | boolean | undefined): number {
  if (value == null || value === true) return 0;
  const port = parseInt(String(value), 10);
  if (isNaN(port) || port < 0 || port > 65535) {
    throw new Error(
      `잘못된 --port 값: ${value} (0~65535 사이의 숫자를 입력하세요)`,
    );
  }
  return port;
}

function helpText(): string {
  return (
    `sleepyrouter ${VERSION}\n\n` +
    `사용법:\n` +
    `  sleepyrouter start [--port 4567]\n` +
    `  sleepyrouter usage [--date YYYYMMDD|--week NN]\n` +
    `  sleepyrouter --version\n`
  );
}

function boolCheck(v: boolean): string {
  return v ? "✓" : "✗";
}

export function runStartCommand(options: {
  port?: number;
  store?: ConfigStore;
}): void {
  const store = options.store ?? new ConfigStore();
  store.ensureRoot();
  const config = store.readConfig();
  let port = options.port || config.port || DEFAULT_PORT;
  if (config.port !== port) {
    config.port = port;
    store.writeConfig(config);
  }

  const env = process.env;
  const keys = resolveProviderAPIKeys(env, store.paths.root);

  console.log(`\nsleepyrouter v${VERSION}`);
  console.log(`  config: ${getConfigPath(store.paths.root)}`);
  console.log(`  env: ${getEnvPath(store.paths.root)}`);
  console.log(`  NVIDIA_API_KEY: ${boolCheck(!!keys.nvidia)}`);
  console.log(`  OPENROUTER_API_KEY: ${boolCheck(!!keys.openRouter)}`);
  console.log(`  OPENCODE_API_KEY: ${boolCheck(!!keys.zen)}`);
  console.log(`  GOOGLE_API_KEY: ${boolCheck(!!keys.google)}`);

  requireAnyProviderAPIKey(env, store.paths.root);

  // Validate model aliases
  const groupNames = Object.keys(config.modelGroups).sort();
  const undefinedAliases: string[] = [];
  for (const group of groupNames) {
    for (const alias of config.modelGroups[group] ?? []) {
      if (config.models && !(alias in config.models)) {
        undefinedAliases.push(alias);
      }
    }
  }

  if (undefinedAliases.length > 0) {
    let msg =
      "\n모델 그룹에 정의되지 않은 alias가 있어요. config.json의 models에 추가하세요:\n";
    for (const m of undefinedAliases) {
      msg += `  - ${m}\n`;
    }
    throw new Error(
      `${msg}: config.json을 수정한 후 다시 시도하세요`,
    );
  }

  if (groupNames.length > 0) {
    const totalModels = allGroupModelIDs(
      config.modelGroups,
      ...(config.groupOrder ?? []),
    ).length;
    console.log(
      `\n모델 그룹 (${totalModels}개 모델, ${groupNames.length}개 그룹)`,
    );
    for (const name of groupNames) {
      const marker =
        name === config.defaultModelGroup ? " (기본)" : "";
      console.log(
        `  ${name}${marker}: ${(config.modelGroups[name] ?? []).join(", ")}`,
      );
    }
    if (config.defaultModelGroup) {
      console.log(`\n기본 그룹: ${config.defaultModelGroup}`);
    }
    console.log();
  }

  const requestLogger = (event: ServerLogEvent) => {
    const parts = [
      `id=${event.id}`,
      `method=${event.method}`,
      `path=${event.path}`,
    ];
    if (event.durationMs != null) parts.push(`duration_ms=${event.durationMs}`);
    if (event.requestedModel) parts.push(`requested=${event.requestedModel}`);
    if (event.modelId) parts.push(`model=${event.modelId}`);
    if (event.group) parts.push(`group=${event.group}`);
    if (event.error) parts.push(`error=${event.error}`);
    if (event.candidateCount != null)
      parts.push(`candidates=${event.candidateCount}`);
    if (event.triedCount != null) parts.push(`tried=${event.triedCount}`);
    if (event.inputTokens != null) parts.push(`in=${event.inputTokens}`);
    if (event.outputTokens != null) parts.push(`out=${event.outputTokens}`);
    if (event.stream) parts.push(`stream=true`);
    if (event.statusCode != null) parts.push(`status=${event.statusCode}`);

    const level = event.type === "response" && (event.statusCode ?? 200) >= 400
      ? "ERROR"
      : "INFO";
    console.log(`[${level}] ${event.type} ${parts.join(" ")}`);
  };

  const server = createSleepyRouterServer({
    store,
    env,
    requestLogger,
  });

  const bunServer = listenServer(server, port);
  console.log(
    `sleepyrouter 서빙 시작: http://127.0.0.1:${bunServer.port}`,
  );
  console.log(`종료하려면 Ctrl+C를 누르세요.\n`);

  // Handle graceful shutdown
  process.on("SIGINT", () => {
    console.log("\nsleepyrouter 종료 중...");
    bunServer.stop();
    store.close();
    process.exit(0);
  });
  process.on("SIGTERM", () => {
    bunServer.stop();
    store.close();
    process.exit(0);
  });
}

export function runUsageCommand(options: {
  date?: string;
  week?: number;
  store?: ConfigStore;
}): void {
  const store = options.store ?? new ConfigStore();
  let logs = store.readUsageLogs();

  // Filter by date/week
  if (options.date) {
    logs = logs.filter((entry) => {
      try {
        const ts = new Date(entry.ts);
        const ymd = `${ts.getFullYear()}${String(ts.getMonth() + 1).padStart(2, "0")}${String(ts.getDate()).padStart(2, "0")}`;
        return ymd === options.date;
      } catch {
        return false;
      }
    });
  } else if (options.week) {
    logs = logs.filter((entry) => {
      try {
        const ts = new Date(entry.ts);
        const startOfYear = new Date(ts.getFullYear(), 0, 1);
        const days = Math.floor(
          (ts.getTime() - startOfYear.getTime()) / 86400000,
        );
        const weekNum = Math.ceil((days + startOfYear.getDay() + 1) / 7);
        return weekNum === options.week;
      } catch {
        return false;
      }
    });
  }

  if (logs.length === 0) {
    let filterDesc = "";
    if (options.date) filterDesc = ` (날짜: ${options.date})`;
    else if (options.week) filterDesc = ` (주차: ${options.week}주차)`;
    console.log(`사용 기록이 없어요${filterDesc}.`);
    return;
  }

  // Aggregate
  const byModel = new Map<
    string,
    {
      model: string;
      requests: number;
      failed: number;
      inputTokens: number;
      outputTokens: number;
    }
  >();
  for (const entry of logs) {
    let row = byModel.get(entry.model);
    if (!row) {
      row = {
        model: entry.model,
        requests: 0,
        failed: 0,
        inputTokens: 0,
        outputTokens: 0,
      };
      byModel.set(entry.model, row);
    }
    row.requests++;
    if (!entry.success) row.failed++;
    row.inputTokens += entry.inputTokens;
    row.outputTokens += entry.outputTokens;
  }

  const rows = [...byModel.values()].sort((a, b) => {
    if (a.requests !== b.requests) return b.requests - a.requests;
    if (a.inputTokens !== b.inputTokens) return b.inputTokens - a.inputTokens;
    return a.model.localeCompare(b.model);
  });

  // Print table
  console.log("\n모델별 사용량:");
  console.log(
    "모델".padEnd(40) +
      "요청".padStart(8) +
      "실패".padStart(8) +
      "입력토큰".padStart(12) +
      "출력토큰".padStart(12),
  );
  console.log("-".repeat(80));

  let totalRequests = 0;
  let totalFailed = 0;
  let totalInput = 0;
  let totalOutput = 0;

  for (const row of rows) {
    totalRequests += row.requests;
    totalFailed += row.failed;
    totalInput += row.inputTokens;
    totalOutput += row.outputTokens;
    console.log(
      row.model.padEnd(40) +
        String(row.requests).padStart(8) +
        String(row.failed).padStart(8) +
        String(row.inputTokens).padStart(12) +
        String(row.outputTokens).padStart(12),
    );
  }
  console.log("-".repeat(80));
  console.log(
    "합계".padEnd(40) +
      String(totalRequests).padStart(8) +
      String(totalFailed).padStart(8) +
      String(totalInput).padStart(12) +
      String(totalOutput).padStart(12),
  );
}

export function main(): void {
  const argv = process.argv.slice(2);
  const { command, flags } = parseArgs(argv);

  switch (command) {
    case "--version":
    case "-v":
    case "version":
      console.log(VERSION);
      return;
    case "help":
    case "--help":
    case "-h":
      console.log(helpText());
      return;
    case "start": {
      try {
        const port = parsePort(flags["port"]);
        runStartCommand({ port: port || undefined });
      } catch (e) {
        console.error(e instanceof Error ? e.message : String(e));
        process.exit(1);
      }
      break;
    }
    case "usage": {
      const date =
        typeof flags["date"] === "string" ? flags["date"] : undefined;
      const week =
        typeof flags["week"] === "string"
          ? parseInt(flags["week"], 10) || undefined
          : undefined;
      runUsageCommand({ date, week });
      break;
    }
    default:
      console.log(helpText());
      process.exit(1);
  }
}
