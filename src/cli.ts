#!/usr/bin/env node
import { runStartCommand } from './commands/start.js';
import { runUsageCommand } from './commands/usage.js';
import { VERSION } from './version.js';

interface ParsedArgs {
  command: string;
  flags: Map<string, string | boolean>;
}

function parseArgs(argv: string[]): ParsedArgs {
  const [command = 'help', ...rest] = argv;
  const flags = new Map<string, string | boolean>();
  for (let i = 0; i < rest.length; i += 1) {
    const arg = rest[i]!;
    if (arg.startsWith('--')) {
      const [name, inline] = arg.slice(2).split('=', 2);
      if (inline !== undefined) flags.set(name!, inline);
      else if (rest[i + 1] && !rest[i + 1]!.startsWith('-')) flags.set(name!, rest[++i]!);
      else flags.set(name!, true);
    }
  }
  return { command, flags };
}

function parsePort(value: string | boolean | undefined): number | undefined {
  if (value === undefined || value === false) return undefined;
  const port = Number(value);
  if (!Number.isInteger(port) || port < 0 || port > 65535) {
    throw new Error(`잘못된 --port 값: ${String(value)} (0~65535 사이의 숫자를 입력하세요)`);
  }
  return port;
}

function help(): void {
  console.log(`sleepyrouter ${VERSION}\n\n사용법:\n  slr start [--port 4567]\n  slr usage [--date YYYYMMDD|--week NN]\n  slr --version\n`);
}

async function main(): Promise<void> {
  const parsed = parseArgs(process.argv.slice(2));
  if (parsed.command === '--version' || parsed.command === '-v' || parsed.command === 'version') {
    console.log(VERSION);
    return;
  }
  if (parsed.command === 'help' || parsed.command === '--help' || parsed.command === '-h') {
    help();
    return;
  }
  if (parsed.command === 'start') {
    const portFlag = parsed.flags.get('port');
    await runStartCommand({
      port: parsePort(portFlag),
    });
    return;
  }
  if (parsed.command === 'usage') {
    const dateFlag = parsed.flags.get('date');
    const weekFlag = parsed.flags.get('week');
    runUsageCommand({
      date: typeof dateFlag === 'string' ? dateFlag : undefined,
      week: typeof weekFlag === 'string' ? Number(weekFlag) : undefined,
    });
    return;
  }
  help();
  process.exitCode = 1;
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
});
