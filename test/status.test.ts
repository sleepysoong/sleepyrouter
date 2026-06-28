import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import { ConfigStore } from '../src/config/store.js';
import { getStatus } from '../src/commands/status.js';

const roots: string[] = [];
afterEach(() => roots.splice(0).forEach((root) => fs.rmSync(root, { recursive: true, force: true })));

describe('status command', () => {
  it('reports primary model from config', () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'slr-status-'));
    roots.push(root);
    const store = new ConfigStore(root);
    store.updateModelGroup('default', ['model-a:free', 'model-b:free']);

    expect(getStatus(store).primaryModel).toBe('model-a:free');
  });

  it('reports no primary model when empty', () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'slr-status-'));
    roots.push(root);
    const store = new ConfigStore(root);

    expect(getStatus(store).primaryModel).toBeUndefined();
  });
});
