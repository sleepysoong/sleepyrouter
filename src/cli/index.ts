import { parseArgs, parsePort } from "./arg-parser.js";
import { runStartCommand } from "./start-command.js";
import { runUsageCommand } from "./usage-command.js";

const VERSION = "0.0.4";

function helpText(): string {
  return (
    `sleepyrouter ${VERSION}\n\n` +
    `사용법:\n` +
    `  sleepyrouter start [--port 4567]\n` +
    `  sleepyrouter usage [--date YYYYMMDD|--week NN]\n` +
    `  sleepyrouter --version\n`
  );
}

export function main(): void {
  const argv = process.argv.slice(2);
  const { command, flags } = parseArgs(argv);

  switch (command) {
    case "--version":
    case "-v":
    case "version":
      console.log(VERSION);
      return;
    case "help":
    case "--help":
    case "-h":
      console.log(helpText());
      return;
    case "start": {
      try {
        const port = parsePort(flags["port"]);
        runStartCommand({ port: port || undefined });
      } catch (e) {
        console.error(e instanceof Error ? e.message : String(e));
        process.exit(1);
      }
      break;
    }
    case "usage": {
      const date =
        typeof flags["date"] === "string" ? flags["date"] : undefined;
      const week =
        typeof flags["week"] === "string"
          ? parseInt(flags["week"], 10) || undefined
          : undefined;
      runUsageCommand({ date, week });
      break;
    }
    default:
      console.log(helpText());
      process.exit(1);
  }
}

export * from "./arg-parser.js";
export * from "./start-command.js";
export * from "./usage-command.js";
