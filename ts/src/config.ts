// Config store - mirrors internal/cfg/config.go + apikey.go
import { Database } from "bun:sqlite";
import { readFileSync, existsSync } from "node:fs";
import type {
  SleepyRouterConfig,
  ModelGroups,
  ProviderAPIKeys,
  UsageLogEntry,
} from "./types.js";
import {
  getConfigRoot,
  getConfigPath,
  getUsagePath,
  getEnvPath,
  readLocalEnv,
  ensureDir,
  writeFileAtomic,
} from "./utils.js";
import {
  normalizeModelGroupsOrdered,
  objectKeysInJSON,
} from "./routing.js";
import { join } from "node:path";

export const DEFAULT_PORT = 4567;

export interface StorePaths {
  root: string;
  configPath: string;
  usagePath: string;
}

function createStorePaths(root: string): StorePaths {
  return {
    root,
    configPath: getConfigPath(root),
    usagePath: getUsagePath(root),
  };
}

export class ConfigStore {
  paths: StorePaths;
  private db: Database | null = null;

  constructor(root?: string) {
    const resolvedRoot = root || getConfigRoot();
    this.paths = createStorePaths(resolvedRoot);
  }

  ensureRoot(): void {
    ensureDir(this.paths.root);
  }

  private usageDBPath(): string {
    return join(this.paths.root, "usage.db");
  }

  private initDB(): Database {
    if (!this.db) {
      this.db = new Database(this.usageDBPath());
      this.db.run(`CREATE TABLE IF NOT EXISTS usage_log (
        ts TEXT NOT NULL,
        model TEXT NOT NULL,
        input_tokens INTEGER NOT NULL,
        output_tokens INTEGER NOT NULL,
        success INTEGER NOT NULL
      )`);
    }
    return this.db;
  }

  readConfig(): SleepyRouterConfig {
    const configPath = this.paths.configPath;
    if (!existsSync(configPath)) {
      return { port: DEFAULT_PORT, modelGroups: {} };
    }
    const raw = JSON.parse(readFileSync(configPath, "utf-8"));

    const config: SleepyRouterConfig = {
      port: DEFAULT_PORT,
      modelGroups: {},
    };

    if (typeof raw.port === "number" && Number.isInteger(raw.port)) {
      config.port = raw.port;
    }

    if (raw.modelGroups) {
      const { groups, order } = normalizeModelGroupsOrdered(raw.modelGroups);
      config.modelGroups = groups;
      // Preserve JSON key order from source
      const jsonKeys = objectKeysInJSON(
        readFileSync(configPath, "utf-8"),
        "modelGroups",
      );
      config.groupOrder = jsonKeys.length > 0 ? jsonKeys : order;
    }

    config.defaultModelGroup =
      raw.defaultModelGroup ?? raw.defaultGroup ?? undefined;
    config.models = raw.models;

    return config;
  }

  writeConfig(config: SleepyRouterConfig): void {
    const data = JSON.stringify(config, null, 2) + "\n";
    writeFileAtomic(this.paths.configPath, data);
  }

  appendUsage(entry: UsageLogEntry): void {
    try {
      const db = this.initDB();
      db.prepare(
        `INSERT INTO usage_log (ts, model, input_tokens, output_tokens, success) VALUES (?, ?, ?, ?, ?)`,
      ).run(
        entry.ts,
        entry.model,
        entry.inputTokens,
        entry.outputTokens,
        entry.success ? 1 : 0,
      );
    } catch {
      // Best-effort usage logging
    }
  }

  readUsageLogs(): UsageLogEntry[] {
    try {
      const db = this.initDB();
      const rows = db
        .prepare(
          `SELECT ts, model, input_tokens as inputTokens, output_tokens as outputTokens, success FROM usage_log ORDER BY ts`,
        )
        .all() as Array<{
        ts: string;
        model: string;
        inputTokens: number;
        outputTokens: number;
        success: number;
      }>;
      return rows.map((r) => ({
        ts: r.ts,
        model: r.model,
        inputTokens: r.inputTokens,
        outputTokens: r.outputTokens,
        success: r.success === 1,
      }));
    } catch {
      return [];
    }
  }

  close(): void {
    this.db?.close();
    this.db = null;
  }
}

// API Key resolution - mirrors internal/cfg/apikey.go

function resolveAPIKey(
  name: string,
  env: Record<string, string | undefined>,
  localEnv: Record<string, string>,
): string {
  const envVal = (env[name] ?? "").trim();
  if (envVal) return envVal;
  return (localEnv[name] ?? "").trim();
}

export function resolveProviderAPIKeys(
  env: Record<string, string | undefined>,
  root: string,
): ProviderAPIKeys {
  const localEnv = readLocalEnv(root);
  return {
    openRouter: resolveAPIKey("OPENROUTER_API_KEY", env, localEnv),
    nvidia: resolveAPIKey("NVIDIA_API_KEY", env, localEnv),
    copilot: resolveAPIKey("GITHUB_COPILOT_TOKEN", env, localEnv),
    zen: resolveAPIKey("OPENCODE_API_KEY", env, localEnv),
    google:
      resolveAPIKey("GOOGLE_API_KEY", env, localEnv) ||
      resolveAPIKey("GEMINI_API_KEY", env, localEnv),
  };
}

export function requireAnyProviderAPIKey(
  env: Record<string, string | undefined>,
  root: string,
): ProviderAPIKeys {
  const keys = resolveProviderAPIKeys(env, root);
  if (
    !keys.openRouter &&
    !keys.nvidia &&
    !keys.copilot &&
    !keys.zen &&
    !keys.google
  ) {
    throw new Error(
      `API 키가 설정되지 않았어요.\n` +
        `  NVIDIA_API_KEY, OPENROUTER_API_KEY, GITHUB_COPILOT_TOKEN, OPENCODE_API_KEY, 또는 GOOGLE_API_KEY 중 하나 이상이 필요해요.\n` +
        `  설정 방법:\n` +
        `    1. 환경변수: export GOOGLE_API_KEY=AIza...\n` +
        `    2. .env 파일: echo "GOOGLE_API_KEY=AIza..." > ${getEnvPath(root)}`,
    );
  }
  return keys;
}
