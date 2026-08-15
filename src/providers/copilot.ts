import { createOpenAICompatible } from "@ai-sdk/openai-compatible";
import type { Provider } from "./base.js";
import { baseURLFrom } from "./base.js";

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

export async function copilotSessionToken(apiKey: string): Promise<string> {
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

export const copilotProvider: Provider = {
  name: "Copilot",
  source: "copilot",
  messageProtocol: "openai",
  chatModel(modelId: string, apiKey: string, customFetch?: typeof fetch) {
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
