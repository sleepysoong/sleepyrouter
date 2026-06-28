import fs from 'node:fs';
import path from 'node:path';
import { normalizeModelGroups } from '../model-groups.js';
import { ModelCache, OmfmConfig, UsageLogEntry } from '../types.js';
import { getConfigPath, getConfigRoot, getModelCachePath, getUsagePath } from './paths.js';

const DEFAULT_PORT = 4567;
export const MODEL_CACHE_TTL_MS = 5 * 60 * 1000;

export interface StorePaths {
  root: string;
  configPath: string;
  usagePath: string;
  modelCachePath: string;
}

export function createStorePaths(root = getConfigRoot()): StorePaths {
  return {
    root,
    configPath: getConfigPath(root),
    usagePath: getUsagePath(root),
    modelCachePath: getModelCachePath(root),
  };
}

function ensureDir(filePath: string): void {
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
}

function readJson<T>(filePath: string, fallback: T): T {
  if (!fs.existsSync(filePath)) return fallback;
  try {
    return JSON.parse(fs.readFileSync(filePath, 'utf8')) as T;
  } catch (error) {
    throw new Error(`${filePath} 파싱에 실패했어요: ${error instanceof Error ? error.message : String(error)}`);
  }
}

function writeJson(filePath: string, value: unknown): void {
  ensureDir(filePath);
  const tmpPath = `${filePath}.${process.pid}.tmp`;
  fs.writeFileSync(tmpPath, `${JSON.stringify(value, null, 2)}\n`);
  fs.renameSync(tmpPath, filePath);
}

export function isModelCacheFresh(cache: ModelCache): boolean {
  return Date.now() - Date.parse(cache.fetchedAt) < MODEL_CACHE_TTL_MS;
}

export class ConfigStore {
  readonly paths: StorePaths;

  constructor(root = getConfigRoot()) {
    this.paths = createStorePaths(root);
  }

  ensureRoot(): void {
    fs.mkdirSync(this.paths.root, { recursive: true });
  }

  readConfig(): OmfmConfig {
    const config = readJson<Partial<OmfmConfig>>(this.paths.configPath, {});
    return {
      port: typeof config.port === 'number' ? config.port : DEFAULT_PORT,
      modelGroups: normalizeModelGroups(config.modelGroups),
      defaultGroup: typeof config.defaultGroup === 'string' ? config.defaultGroup : undefined,
    };
  }

  writeConfig(config: OmfmConfig): void {
    writeJson(this.paths.configPath, config);
  }

  updateModelGroup(group: string, modelIds: string[]): OmfmConfig {
    const config = this.readConfig();
    const groupIds = [...new Set(modelIds)];
    const next = {
      ...config,
      modelGroups: { ...config.modelGroups, [group]: groupIds },
    };
    this.writeConfig(next);
    return next;
  }

  // ponytail: JSONL append-log — 한 줄에 하나의 요청 기록
  appendUsage(entry: UsageLogEntry): void {
    ensureDir(this.paths.usagePath);
    // backup old usage.json if exists
    const oldJson = this.paths.usagePath.replace(/\.jsonl$/, '.json');
    if (fs.existsSync(oldJson)) {
      fs.renameSync(oldJson, `${oldJson}.bak`);
    }
    fs.appendFileSync(this.paths.usagePath, `${JSON.stringify(entry)}\n`);
  }

  readUsageLogs(): UsageLogEntry[] {
    if (!fs.existsSync(this.paths.usagePath)) return [];
    const text = fs.readFileSync(this.paths.usagePath, 'utf8').trim();
    if (!text) return [];
    const lines = text.split('\n');
    const result: UsageLogEntry[] = [];
    for (const line of lines) {
      if (!line) continue;
      try {
        result.push(JSON.parse(line) as UsageLogEntry);
      } catch {
        // skip malformed
      }
    }
    return result;
  }

  readModelCache(): ModelCache | undefined {
    return readJson<ModelCache | undefined>(this.paths.modelCachePath, undefined);
  }

  writeModelCache(cache: ModelCache): void {
    writeJson(this.paths.modelCachePath, cache);
  }
}

export { DEFAULT_PORT };
