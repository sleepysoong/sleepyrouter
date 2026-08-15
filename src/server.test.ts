import { describe, expect, test, beforeEach, afterEach } from "bun:test";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { createSleepyRouterServer } from "./server.js";
import { ConfigStore } from "./config.js";

describe("HTTP Server & Router", () => {
  let tmpDir: string;
  let store: ConfigStore;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), "server-test-"));
    store = new ConfigStore(tmpDir);
    store.ensureRoot();
  });

  afterEach(() => {
    store.close();
    try {
      rmSync(tmpDir, { recursive: true, force: true });
    } catch {}
  });

  test("GET /health returns health info", async () => {
    const server = createSleepyRouterServer({ store, env: { OPENROUTER_API_KEY: "test" } });
    const req = new Request("http://localhost/health");
    const res = await server.fetch(req);
    expect(res.status).toBe(200);
    const json = await res.json();
    expect(json.ok).toBe(true);
    expect(json.service).toBe("sleepyrouter");
  });

  test("GET /v1/models returns empty list when no models configured", async () => {
    const server = createSleepyRouterServer({ store, env: { OPENROUTER_API_KEY: "test" } });
    const req = new Request("http://localhost/v1/models");
    const res = await server.fetch(req);
    expect(res.status).toBe(200);
    const json = await res.json();
    expect(json.object).toBe("list");
    expect(json.data).toEqual([]);
  });

  test("GET /v1/models returns model definitions when configured", async () => {
    store.writeConfig({
      port: 4567,
      modelGroups: { fast: ["fast-model"] },
      models: {
        "fast-model": { provider: "openrouter", name: "openai/gpt-4o-mini" },
      },
    });

    const server = createSleepyRouterServer({ store, env: { OPENROUTER_API_KEY: "test" } });
    const req = new Request("http://localhost/v1/models");
    const res = await server.fetch(req);
    expect(res.status).toBe(200);
    const json = await res.json();
    expect(json.data.length).toBe(1);
    expect(json.data[0].id).toBe("fast-model");
    expect(json.data[0].owned_by).toBe("openrouter");
  });

  test("POST /anthropic/v1/messages/count_tokens", async () => {
    const server = createSleepyRouterServer({ store, env: { OPENROUTER_API_KEY: "test" } });
    const req = new Request("http://localhost/anthropic/v1/messages/count_tokens", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ messages: [{ role: "user", content: "Hello world" }] }),
    });
    const res = await server.fetch(req);
    expect(res.status).toBe(200);
    const json = await res.json();
    expect(typeof json.input_tokens).toBe("number");
  });

  test("404 for unknown endpoints", async () => {
    const server = createSleepyRouterServer({ store, env: { OPENROUTER_API_KEY: "test" } });
    const req = new Request("http://localhost/unknown/route");
    const res = await server.fetch(req);
    expect(res.status).toBe(404);
  });

  test("POST /v1/chat/completions with missing models returns 400", async () => {
    const server = createSleepyRouterServer({ store, env: { OPENROUTER_API_KEY: "test" } });
    const req = new Request("http://localhost/v1/chat/completions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ messages: [{ role: "user", content: "Hi" }] }),
    });
    const res = await server.fetch(req);
    expect(res.status).toBe(400);
    const json = await res.json();
    expect(json.error.message).toContain("선택된 무료 모델이 없어요");
  });

  test("POST /v1/chat/completions throws 500 when no API keys are configured", async () => {
    const server = createSleepyRouterServer({ store, env: {} });
    const req = new Request("http://localhost/v1/chat/completions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ messages: [{ role: "user", content: "Hi" }] }),
    });
    const res = await server.fetch(req);
    expect(res.status).toBe(500);
  });
});
