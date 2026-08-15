import {
  ConfigStore,
  DEFAULT_PORT,
  resolveProviderAPIKeys,
  requireAnyProviderAPIKey,
} from "../config/index.js";
import { createSleepyRouterServer, listenServer } from "../server/index.js";
import { allGroupModelIDs } from "../routing/index.js";
import { getConfigPath, getEnvPath } from "../utils.js";
import type { ServerLogEvent } from "../handlers/index.js";

const VERSION = "0.0.4";

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

    const level =
      event.type === "response" && (event.statusCode ?? 200) >= 400
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
