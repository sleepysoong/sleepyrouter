import { readFileSync, existsSync } from "node:fs";
import type { SleepyRouterConfig, UsageLogEntry } from "../types.js";
import {
  getConfigRoot,
  getConfigPath,
  getUsagePath,
  ensureDir,
  writeFileAtomic,
} from "../utils.js";
import { normalizeModelGroupsOrdered, objectKeysInJSON } from "../routing/index.js";
import { UsageLogger } from "./usage-logger.js";

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
  private logger: UsageLogger;

  constructor(root?: string) {
    const resolvedRoot = root || getConfigRoot();
    this.paths = createStorePaths(resolvedRoot);
    this.logger = new UsageLogger(this.paths.root);
  }

  ensureRoot(): void {
    ensureDir(this.paths.root);
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
    this.logger.appendUsage(entry);
  }

  readUsageLogs(): UsageLogEntry[] {
    return this.logger.readUsageLogs();
  }

  close(): void {
    this.logger.close();
  }
}
