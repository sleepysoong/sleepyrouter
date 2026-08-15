import { createOpenAICompatible } from "@ai-sdk/openai-compatible";
import type { Provider } from "./base.js";
import { baseURLFrom } from "./base.js";

export const nvidiaProvider: Provider = {
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
