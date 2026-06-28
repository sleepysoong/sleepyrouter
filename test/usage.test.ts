import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { runUsageCommand } from '../src/commands/usage.js';
import { ConfigStore } from '../src/config/store.js';

const roots: string[] = [];
afterEach(() => roots.splice(0).forEach((root) => fs.rmSync(root, { recursive: true, force: true })));

function tempStore(): ConfigStore {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'slr-usage-'));
  roots.push(root);
  return new ConfigStore(root);
}

describe('usage command', () => {
  it('prints empty when no records', () => {
    const store = tempStore();
    const out: string[] = [];
    const spy = vi.spyOn(console, 'log').mockImplementation((msg) => out.push(msg));
    try {
      runUsageCommand({ store });
      expect(out.join('')).toContain('기록');
    } finally {
      spy.mockRestore();
    }
  });

  it('prints usage aggregated by model', () => {
    const store = tempStore();
    store.appendUsage({ ts: '2026-06-28T10:00:00Z', model: 'beta', inputTokens: 0, outputTokens: 0, success: true });
    store.appendUsage({ ts: '2026-06-28T10:01:00Z', model: 'alpha', inputTokens: 1, outputTokens: 2, success: true });
    store.appendUsage({ ts: '2026-06-28T10:02:00Z', model: 'alpha', inputTokens: 0, outputTokens: 0, success: false });
    const out: string[] = [];
    const spy = vi.spyOn(console, 'log').mockImplementation((msg) => out.push(msg));
    try {
      runUsageCommand({ store });
      const text = out.join('\n');
      expect(text).toContain('Model ID');
      expect(text.indexOf('alpha')).toBeLessThan(text.indexOf('beta'));
      expect(text).toContain('1');  // inputTokens for alpha (1+0)
    } finally {
      spy.mockRestore();
    }
  });
});
