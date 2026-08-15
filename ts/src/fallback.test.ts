import { describe, expect, test, beforeEach, afterEach } from "bun:test";
import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { createSleepyRouterServer } from "./server.js";
import { ConfigStore } from "./config.js";
import { registerProvider, type Provider } from "./providers.js";
import { createOpenAICompatible } from "@ai-sdk/openai-compatible";

describe("Candidate Failover & Retry Logic", () => {
  let tmpDir: string;
  let store: ConfigStore;

  beforeEach(() => {
    tmpDir = mkdtempSync(join(tmpdir(), "fallback-test-"));
    store = new ConfigStore(tmpDir);
    store.ensureRoot();
  });

  afterEach(() => {
    store.close();
    try {
      rmSync(tmpDir, { recursive: true, force: true });
    } catch {}
  });

  test("returns 502 when all candidate models fail", async () => {
    const mockFetch = async (): Promise<Response> => {
      return new Response(JSON.stringify({ error: { message: "Internal server error" } }), {
        status: 500,
        headers: { "Content-Type": "application/json" },
      });
    };

    const failProvider: Provider = {
      name: "FailProvider",
      source: "openrouter",
      messageProtocol: "openai",
      chatModel(_modelId: string, _apiKey: string) {
        const p = createOpenAICompatible({
          name: "fail",
          baseURL: "http://mock-upstream/v1",
          apiKey: "sk-test",
          fetch: mockFetch as any,
        });
        return p.chatModel("fail-model");
      },
    };
    registerProvider("openrouter", failProvider);

    store.writeConfig({
      port: 4567,
      modelGroups: { high: ["model-fail-1", "model-fail-2"] },
      defaultModelGroup: "high",
      models: {
        "model-fail-1": { provider: "openrouter", name: "fail/model-1" },
        "model-fail-2": { provider: "openrouter", name: "fail/model-2" },
      },
    });

    const server = createSleepyRouterServer({
      store,
      env: { OPENROUTER_API_KEY: "sk-or-test" },
    });

    const req = new Request("http://localhost/v1/chat/completions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        model: "high",
        messages: [{ role: "user", content: "hello" }],
      }),
    });

    const res = await server.fetch(req);
    expect(res.status).toBe(502);
    const json = await res.json();
    expect(json.error.message).toBe("선택된 모든 무료 모델이 실패했어요.");
    expect(json.error.details).toBeDefined();
  });

  test("fails over from candidate 1 to candidate 2 on error", async () => {
    const attemptedModels: string[] = [];

    const mockFetch = async (url: string | URL | Request, init?: RequestInit): Promise<Response> => {
      const bodyStr = String(init?.body ?? "");
      if (bodyStr.includes("model-1")) {
        attemptedModels.push("model-1");
        return new Response(JSON.stringify({ error: { message: "Rate limit exceeded" } }), {
          status: 429,
          headers: { "Content-Type": "application/json" },
        });
      }
      attemptedModels.push("model-2");
      return new Response(
        JSON.stringify({
          id: "chatcmpl-success",
          object: "chat.completion",
          choices: [{ message: { role: "assistant", content: "Success response!" }, finish_reason: "stop" }],
          usage: { prompt_tokens: 10, completion_tokens: 5 },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    };

    const failoverProvider: Provider = {
      name: "FailoverProvider",
      source: "openrouter",
      messageProtocol: "openai",
      chatModel(modelId: string, _apiKey: string) {
        const p = createOpenAICompatible({
          name: "failover",
          baseURL: "http://mock-upstream/v1",
          apiKey: "sk-test",
          fetch: mockFetch as any,
        });
        return p.chatModel(modelId);
      },
    };
    registerProvider("openrouter", failoverProvider);

    store.writeConfig({
      port: 4567,
      modelGroups: { high: ["model-1", "model-2"] },
      defaultModelGroup: "high",
      models: {
        "model-1": { provider: "openrouter", name: "upstream/model-1" },
        "model-2": { provider: "openrouter", name: "upstream/model-2" },
      },
    });

    const server = createSleepyRouterServer({
      store,
      env: { OPENROUTER_API_KEY: "sk-or-test" },
    });

    const req = new Request("http://localhost/v1/chat/completions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        model: "high",
        messages: [{ role: "user", content: "hello" }],
      }),
    });

    const res = await server.fetch(req);
    expect(res.status).toBe(200);
    const json = await res.json();
    expect(json.choices[0].message.content).toBe("Success response!");
    expect(attemptedModels).toEqual(["model-1", "model-2"]);
  });
});
