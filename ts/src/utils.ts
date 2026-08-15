// Utility helpers - mirrors internal/utils/env.go + http.go
import { readFileSync, existsSync, mkdirSync, writeFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { homedir } from "node:os";

const CONFIG_FILE_NAME = "config.json";
const USAGE_FILE_NAME = "usage.jsonl";

export function getConfigRoot(): string {
  const envRoot = process.env["SLEEPYROUTER_HOME"];
  if (envRoot) return envRoot;
  return join(homedir(), ".sleepyrouter");
}

export function getConfigPath(root: string): string {
  return join(root, CONFIG_FILE_NAME);
}

export function getUsagePath(root: string): string {
  return join(root, USAGE_FILE_NAME);
}

export function getEnvPath(root: string): string {
  return join(root, ".env");
}

export function parseDotEnv(content: string): Record<string, string> {
  const values: Record<string, string> = {};
  for (const rawLine of content.replace(/\r\n/g, "\n").split("\n")) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) continue;
    const idx = line.indexOf("=");
    if (idx < 0) continue;
    const key = line.slice(0, idx).trim();
    if (!key) continue;
    let value = line.slice(idx + 1).trim();
    if (
      value.length >= 2 &&
      ((value[0] === '"' && value[value.length - 1] === '"') ||
        (value[0] === "'" && value[value.length - 1] === "'"))
    ) {
      value = value.slice(1, -1);
    }
    values[key] = value;
  }
  return values;
}

export function readLocalEnv(root: string): Record<string, string> {
  const envPath = getEnvPath(root);
  try {
    return parseDotEnv(readFileSync(envPath, "utf-8"));
  } catch {
    return {};
  }
}

export function stringFromUnknown(value: unknown): string {
  if (typeof value === "string") return value;
  return "";
}

export function boolValue(value: unknown): boolean {
  if (value == null) return false;
  if (typeof value === "boolean") return value;
  if (typeof value === "number") return value !== 0;
  if (typeof value === "string") return value !== "";
  return true;
}

export function numberValue(value: unknown): number | null {
  if (typeof value === "number" && value >= 0 && Number.isInteger(value))
    return value;
  return null;
}

export function intPointer(v: number): number {
  return v;
}

export function cloneObject<T extends Record<string, unknown>>(value: T): T {
  return { ...value };
}

export function ensureDir(dir: string): void {
  mkdirSync(dir, { recursive: true });
}

export function writeFileAtomic(path: string, data: string): void {
  const dir = dirname(path);
  ensureDir(dir);
  const tmp = join(dir, `.sleepyrouter-${Date.now()}.tmp`);
  try {
    writeFileSync(tmp, data);
    const { renameSync } = require("node:fs");
    renameSync(tmp, path);
  } catch (e) {
    try {
      const { unlinkSync } = require("node:fs");
      unlinkSync(tmp);
    } catch {}
    throw e;
  }
}

export function truncate(s: string, max: number): string {
  return s.length > max ? s.slice(0, max) : s;
}

export function safeLogValue(value: string): string {
  const sanitized = value.replace(/[\x00-\x1f\x7f]/g, "?");
  return sanitized.length > 200 ? sanitized.slice(0, 197) + "..." : sanitized;
}
