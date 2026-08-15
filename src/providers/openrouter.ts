import { createOpenAICompatible } from "@ai-sdk/openai-compatible";
import type { Provider } from "./base.js";
import { baseURLFrom } from "./base.js";

export const openRouterProvider: Provider = {
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
