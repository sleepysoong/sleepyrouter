import { describe, expect, test, beforeEach, afterEach } from "bun:test";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import {
  ConfigStore,
  resolveProviderAPIKeys,
  requireAnyProviderAPIKey,
} from "./config.js";
import { parseDotEnv } from "./utils.js";

describe("Config & Env", () => {
  let tmpDir: string;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), "sleepy-test-"));
  });

  afterEach(() => {
    try {
      rmSync(tmpDir, { recursive: true, force: true });
    } catch {}
  });

  test("parseDotEnv handles quotes, comments, and empty lines", () => {
    const dotenv = `
    # Comment
    OPENROUTER_API_KEY="sk-or-test"
    NVIDIA_API_KEY='nvapi-test'
    PLAIN_KEY=plain_val
    `;
    const parsed = parseDotEnv(dotenv);
    expect(parsed).toEqual({
      OPENROUTER_API_KEY: "sk-or-test",
      NVIDIA_API_KEY: "nvapi-test",
      PLAIN_KEY: "plain_val",
    });
  });

  test("ConfigStore default fallback when config does not exist", () => {
    const store = new ConfigStore(tmpDir);
    const cfg = store.readConfig();
    expect(cfg.port).toBe(4567);
    expect(cfg.modelGroups).toEqual({});
  });

  test("ConfigStore read and write config", () => {
    const store = new ConfigStore(tmpDir);
    store.ensureRoot();
    const newCfg = {
      port: 8080,
      modelGroups: { fast: ["model-1"] },
      defaultModelGroup: "fast",
    };
    store.writeConfig(newCfg);

    const reloaded = store.readConfig();
    expect(reloaded.port).toBe(8080);
    expect(reloaded.modelGroups).toEqual({ fast: ["model-1"] });
    expect(reloaded.defaultModelGroup).toBe("fast");
  });

  test("ConfigStore usage db append and read", () => {
    const store = new ConfigStore(tmpDir);
    store.ensureRoot();

    store.appendUsage({
      ts: "2026-08-15T12:00:00Z",
      model: "test-model",
      inputTokens: 100,
      outputTokens: 50,
      success: true,
    });

    const logs = store.readUsageLogs();
    expect(logs.length).toBe(1);
    expect(logs[0]!.model).toBe("test-model");
    expect(logs[0]!.inputTokens).toBe(100);
    expect(logs[0]!.outputTokens).toBe(50);
    expect(logs[0]!.success).toBe(true);

    store.close();
  });

  test("resolveProviderAPIKeys reads env and local .env file", () => {
    const envFile = join(tmpDir, ".env");
    writeFileSync(envFile, "OPENROUTER_API_KEY=sk-local\nGOOGLE_API_KEY=google-local\n");

    const env: Record<string, string | undefined> = {
      NVIDIA_API_KEY: "nv-env",
    };

    const keys = resolveProviderAPIKeys(env, tmpDir);
    expect(keys.openRouter).toBe("sk-local");
    expect(keys.nvidia).toBe("nv-env");
    expect(keys.google).toBe("google-local");
  });

  test("requireAnyProviderAPIKey throws when no keys present", () => {
    expect(() => requireAnyProviderAPIKey({}, tmpDir)).toThrow("API 키가 설정되지 않았어요");
  });
});
