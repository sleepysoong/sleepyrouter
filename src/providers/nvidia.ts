import { createOpenAICompatible } from "@ai-sdk/openai-compatible";
import type { ProviderAdapter } from "./base.js";
import { baseURLFrom } from "./base.js";

export const nvidiaProvider: ProviderAdapter = {
  name: "NVIDIA",
  source: "nvidia",
  apiKeyEnvVar: "NVIDIA_API_KEY",
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
