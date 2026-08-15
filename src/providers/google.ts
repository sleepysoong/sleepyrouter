import { createOpenAICompatible } from "@ai-sdk/openai-compatible";
import type { Provider } from "./base.js";
import { baseURLFrom } from "./base.js";

export const googleProvider: Provider = {
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
