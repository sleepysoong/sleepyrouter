import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';
import { getDoctorStatus, printDoctorStatus } from '../src/commands/doctor.js';
import { ConfigStore } from '../src/config/store.js';

const roots: string[] = [];
function tempStore(): ConfigStore {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'slr-doctor-'));
  roots.push(root);
  return new ConfigStore(root);
}

function output() {
  let text = '';
  return {
    stream: { write: (chunk: string) => { text += chunk; } },
    text: () => text,
  };
}

afterEach(() => roots.splice(0).forEach((root) => fs.rmSync(root, { recursive: true, force: true })));

describe('doctor command', () => {
  it('reports provider key sources, cache, and model counts', () => {
    const store = tempStore();
    store.updateModelGroup('default', ['a']);
    store.writeModelCache({ models: [{ id: 'a', name: 'A', provider: 'alpha' }], fetchedAt: '2026-05-03T00:00:00.000Z' });
    fs.writeFileSync(path.join(store.paths.root, '.env'), 'NVIDIA_API_KEY=nvapi-local\n');

    const status = getDoctorStatus({ store, env: { OPENROUTER_API_KEY: 'sk-or-process' } as NodeJS.ProcessEnv });
    expect(status.providers).toEqual([
      expect.objectContaining({ name: 'OpenRouter', source: 'process', validPrefix: true }),
      expect.objectContaining({ name: 'NVIDIA', source: 'local-env', validPrefix: true }),
    ]);
    expect(status.modelCount).toBe(1);
    expect(status.cachedModelCount).toBe(1);

    const out = output();
    printDoctorStatus({ store, env: { OPENROUTER_API_KEY: 'sk-or-process' } as NodeJS.ProcessEnv, stdout: out.stream });
    expect(out.text()).toContain('slr 진단');
    expect(out.text()).toContain('OpenRouter: process, 접두사 확인됨');
    expect(out.text()).toContain('NVIDIA: local-env, 접두사 확인됨');
  });
});
