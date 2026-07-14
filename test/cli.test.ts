import { execFileSync } from 'node:child_process';
import { describe, expect, it } from 'vitest';

describe('CLI entrypoint', () => {
  it('prints help', () => {
    const out = execFileSync(process.execPath, ['--import', 'tsx', 'src/cli.ts', '--help'], { encoding: 'utf8' });
    expect(out).toContain('sleepyrouter');
    expect(out).toContain('slr start');
  });

  it('prints version with --version', () => {
    const out = execFileSync(process.execPath, ['--import', 'tsx', 'src/cli.ts', '--version'], { encoding: 'utf8' });
    expect(out.trim()).toMatch(/^\d+\.\d+\.\d+/);
  });
});
