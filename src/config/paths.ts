import os from 'node:os';
import path from 'node:path';

export function getConfigRoot(env: NodeJS.ProcessEnv = process.env): string {
  return env.SLR_HOME || path.join(os.homedir(), '.sleepy-llm-router');
}

export function getConfigPath(root = getConfigRoot()): string {
  return path.join(root, 'config.json');
}

export function getUsagePath(root = getConfigRoot()): string {
  return path.join(root, 'usage.jsonl');
}

export function getModelCachePath(root = getConfigRoot()): string {
  return path.join(root, 'models-cache.json');
}

export function getEnvPath(root = getConfigRoot()): string {
  return path.join(root, '.env');
}

export function getLogPath(root = getConfigRoot()): string {
  return path.join(root, 'slr.log');
}
