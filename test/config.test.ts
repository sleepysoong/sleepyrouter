import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { parseDotEnv, resolveNvidiaApiKey, resolveOpenRouterApiKey } from '../src/config/env.js';
import { ConfigStore, MODEL_CACHE_TTL_MS, isModelCacheFresh } from '../src/config/store.js';

const roots: string[] = [];
function tempRoot() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'slr-test-'));
  roots.push(root);
  return root;
}
afterEach(() => roots.splice(0).forEach((root) => fs.rmSync(root, { recursive: true, force: true })));

describe('config/env', () => {
  it('uses process OPENROUTER_API_KEY before local .env', () => {
    const root = tempRoot();
    fs.writeFileSync(path.join(root, '.env'), 'OPENROUTER_API_KEY=local\n');
    expect(resolveOpenRouterApiKey({ OPENROUTER_API_KEY: 'global' } as NodeJS.ProcessEnv, root)).toBe('global');
  });

  it('falls back to ~/.sleepy-llm-router/.env equivalent', () => {
    const root = tempRoot();
    fs.writeFileSync(path.join(root, '.env'), 'OPENROUTER_API_KEY="local-key"\n');
    expect(resolveOpenRouterApiKey({} as NodeJS.ProcessEnv, root)).toBe('local-key');
  });

  it('resolves NVIDIA_API_KEY from process and local env', () => {
    const root = tempRoot();
    fs.writeFileSync(path.join(root, '.env'), 'NVIDIA_API_KEY=local-nv\n');
    expect(resolveNvidiaApiKey({ NVIDIA_API_KEY: 'global-nv' } as NodeJS.ProcessEnv, root)).toBe('global-nv');
    expect(resolveNvidiaApiKey({} as NodeJS.ProcessEnv, root)).toBe('local-nv');
  });

  it('parses dotenv comments and quotes', () => {
    expect(parseDotEnv('# hi\nA=1\nB="two"\n')).toEqual({ A: '1', B: 'two' });
  });

  it('persists model groups', () => {
    const store = new ConfigStore(tempRoot());
    store.updateModelGroup('fast', ['b', 'b']);
    store.updateModelGroup('slow', ['a']);
    const again = new ConfigStore(store.paths.root);
    expect(again.readConfig().modelGroups.fast).toEqual(['b']);
    expect(again.readConfig().modelGroups.slow).toEqual(['a']);
  });

  it('appends usage log entries', () => {
    const store = new ConfigStore(tempRoot());
    store.appendUsage({ ts: '2026-06-28T10:00:00Z', model: 'a', inputTokens: 3, outputTokens: 4, success: true });
    store.appendUsage({ ts: '2026-06-28T10:01:00Z', model: 'a', inputTokens: 0, outputTokens: 0, success: false });
    const logs = new ConfigStore(store.paths.root).readUsageLogs();
    expect(logs).toHaveLength(2);
    expect(logs[0]).toEqual({ ts: '2026-06-28T10:00:00Z', model: 'a', inputTokens: 3, outputTokens: 4, success: true });
    expect(logs[1]).toEqual({ ts: '2026-06-28T10:01:00Z', model: 'a', inputTokens: 0, outputTokens: 0, success: false });
  });

  it('defaults missing model groups for existing configs', () => {
    const store = new ConfigStore(tempRoot());
    fs.mkdirSync(store.paths.root, { recursive: true });
    fs.writeFileSync(store.paths.configPath, '{"port":1234}\n');
    expect(store.readConfig()).toMatchObject({
      port: 1234,
      modelGroups: {},
    });
  });

  it('treats model cache as fresh for 5 minutes', () => {
    const now = Date.parse('2026-05-03T00:00:00.000Z');
    vi.useFakeTimers();
    vi.setSystemTime(now);
    try {
      expect(isModelCacheFresh({ models: [], fetchedAt: new Date(now - MODEL_CACHE_TTL_MS + 1).toISOString() })).toBe(true);
      expect(isModelCacheFresh({ models: [], fetchedAt: new Date(now - MODEL_CACHE_TTL_MS).toISOString() })).toBe(false);
      expect(isModelCacheFresh({ models: [], fetchedAt: 'not-a-date' })).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });
});
