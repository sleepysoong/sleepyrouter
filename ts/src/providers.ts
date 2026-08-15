// Provider registry using Vercel AI SDK - mirrors internal/providers/
import { createOpenAICompatible } from "@ai-sdk/openai-compatible";
import type { LanguageModel } from "ai";
import type { ModelSource, SleepyRouterModel } from "./types.js";

export type MessageProtocol = "openai" | "anthropic";

export interface Provider {
  name: string;
  source: ModelSource;
  messageProtocol: MessageProtocol;
  chatModel(modelId: string, apiKey: string, customFetch?: typeof fetch): LanguageModel;
}

const providers = new Map<ModelSource, Provider>();

export function registerProvider(source: ModelSource, p: Provider): void {
  providers.set(source, p);
}

export function getProvider(source: ModelSource): Provider | undefined {
  return providers.get(source);
}

// Helper: base URL from env or default
function baseURLFrom(envVar: string, def: string): string {
  return process.env[envVar] || def;
}

// ---- OpenRouter Provider ----

const openRouterProvider: Provider = {
  name: "OpenRouter",
  source: "openrouter",
  messageProtocol: "anthropic",
  chatModel(modelId: string, apiKey: string, customFetch?: typeof fetch) {
    const provider = createOpenAICompatible({
      name: "openrouter",
      baseURL: baseURLFrom(
        "SLEEPYROUTER_OPENROUTER_BASE_URL",
        "https://openrouter.ai/api/v1",
      ),
      apiKey,
      headers: {
        "HTTP-Referer": "https://github.com/sleepysoong/sleepyrouter",
        "X-OpenRouter-Title": "sleepyrouter",
      },
      fetch: customFetch,
    });
    return provider.chatModel(modelId);
  },
};

// ---- NVIDIA Provider ----

const nvidiaProvider: Provider = {
  name: "NVIDIA",
  source: "nvidia",
  messageProtocol: "openai",
  chatModel(modelId: string, apiKey: string, customFetch?: typeof fetch) {
    const provider = createOpenAICompatible({
      name: "nvidia",
      baseURL: baseURLFrom(
        "SLEEPYROUTER_NVIDIA_BASE_URL",
        "https://integrate.api.nvidia.com/v1",
      ),
      apiKey,
      fetch: customFetch,
    });
    return provider.chatModel(modelId);
  },
};

// ---- Copilot Provider ----

const COPILOT_TOKEN_URL_DEFAULT =
  "https://api.github.com/copilot_internal/v2/token";
const VERSION = "0.0.4";

interface CopilotToken {
  token: string;
  expiresAt: Date;
}

let copilotTokenCache: CopilotToken | null = null;

async function exchangeCopilotToken(apiKey: string): Promise<CopilotToken> {
  const url = baseURLFrom(
    "SLEEPYROUTER_COPILOT_TOKEN_URL",
    COPILOT_TOKEN_URL_DEFAULT,
  );
  const resp = await fetch(url, {
    headers: {
      Authorization: `token ${apiKey}`,
      "User-Agent": `sleepyrouter/${VERSION}`,
    },
  });
  if (!resp.ok) {
    throw new Error(
      `copilot 토큰 교환 실패: ${resp.status} ${resp.statusText} (GET copilot_internal/v2/token)`,
    );
  }
  const body = await resp.json();
  const token = body.token as string;
  const expiresAt = body.expires_at as number;
  if (!token || !expiresAt) {
    throw new Error(
      "copilot 토큰 응답에 token 또는 expires_at 필드가 없어요",
    );
  }
  return { token, expiresAt: new Date(expiresAt * 1000) };
}

async function copilotSessionToken(apiKey: string): Promise<string> {
  if (
    copilotTokenCache &&
    Date.now() < copilotTokenCache.expiresAt.getTime() - 5 * 60 * 1000
  ) {
    return copilotTokenCache.token;
  }
  const token = await exchangeCopilotToken(apiKey);
  copilotTokenCache = token;
  return token.token;
}

const copilotProvider: Provider = {
  name: "Copilot",
  source: "copilot",
  messageProtocol: "openai",
  chatModel(modelId: string, apiKey: string, customFetch?: typeof fetch) {
    // apiKey here is the PAT; session token exchange happens before calling.
    const provider = createOpenAICompatible({
      name: "copilot",
      baseURL: baseURLFrom(
        "SLEEPYROUTER_COPILOT_BASE_URL",
        "https://api.githubcopilot.com",
      ),
      apiKey,
      headers: {
        "Copilot-Integration-Id": "vscode-chat",
        "Editor-Version": "vscode/1.99.0",
        "Editor-Plugin-Version": "copilot-chat/0.26.7",
        "x-github-api-version": "2025-04-01",
      },
      fetch: customFetch,
    });
    return provider.chatModel(modelId);
  },
};

// Exported for use in the handler to get session token before calling
export { copilotSessionToken };

// ---- Google Provider ----

const googleProvider: Provider = {
  name: "Google",
  source: "google",
  messageProtocol: "openai",
  chatModel(modelId: string, apiKey: string, customFetch?: typeof fetch) {
    const provider = createOpenAICompatible({
      name: "google",
      baseURL: baseURLFrom(
        "SLEEPYROUTER_GOOGLE_BASE_URL",
        "https://generativelanguage.googleapis.com/v1beta/openai",
      ),
      apiKey,
      fetch: customFetch,
    });
    return provider.chatModel(modelId);
  },
};

// ---- Zen Provider ----

const zenProvider: Provider = {
  name: "Zen",
  source: "zen",
  messageProtocol: "openai",
  chatModel(modelId: string, apiKey: string, customFetch?: typeof fetch) {
    const provider = createOpenAICompatible({
      name: "zen",
      baseURL: baseURLFrom(
        "SLEEPYROUTER_ZEN_BASE_URL",
        "https://api.zen.dev/v1",
      ),
      apiKey,
      fetch: customFetch,
    });
    return provider.chatModel(modelId);
  },
};

// ---- Register all providers ----

registerProvider("openrouter", openRouterProvider);
registerProvider("nvidia", nvidiaProvider);
registerProvider("copilot", copilotProvider);
registerProvider("google", googleProvider);
registerProvider("zen", zenProvider);
