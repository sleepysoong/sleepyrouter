import { createOpenAICompatible } from "@ai-sdk/openai-compatible";
import type { ProviderAdapter } from "./base.js";
import { baseURLFrom } from "./base.js";

export const zenProvider: ProviderAdapter = {
  name: "Zen",
  source: "zen",
  apiKeyEnvVar: "OPENCODE_API_KEY",
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
