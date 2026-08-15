export function parseArgs(argv: string[]): {
  command: string;
  flags: Record<string, string | boolean>;
} {
  if (argv.length === 0) return { command: "help", flags: {} };
  const command = argv[0]!;
  const flags: Record<string, string | boolean> = {};
  for (let i = 1; i < argv.length; i++) {
    const arg = argv[i]!;
    if (arg.startsWith("--")) {
      const rest = arg.slice(2);
      const eqIdx = rest.indexOf("=");
      if (eqIdx >= 0) {
        flags[rest.slice(0, eqIdx)] = rest.slice(eqIdx + 1);
      } else if (i + 1 < argv.length && !argv[i + 1]!.startsWith("-")) {
        i++;
        flags[rest] = argv[i]!;
      } else {
        flags[rest] = true;
      }
    }
  }
  return { command, flags };
}

export function parsePort(value: string | boolean | undefined): number {
  if (value == null || value === true) return 0;
  const port = parseInt(String(value), 10);
  if (isNaN(port) || port < 0 || port > 65535) {
    throw new Error(
      `잘못된 --port 값: ${value} (0~65535 사이의 숫자를 입력하세요)`,
    );
  }
  return port;
}
