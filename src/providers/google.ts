import { createGoogle } from "@ai-sdk/google";
import type { ProviderAdapter } from "./base.js";

export const googleProvider: ProviderAdapter = {
  name: "Google",
  source: "google",
  apiKeyEnvVar: "GOOGLE_API_KEY",
  messageProtocol: "openai",
  chatModel(modelId: string, apiKey: string, customFetch?: typeof fetch) {
    const customBaseURL = process.env["SLEEPYROUTER_GOOGLE_BASE_URL"];
    const google = createGoogle({
      apiKey,
      baseURL: customBaseURL || undefined,
      fetch: customFetch,
    });
    return google.languageModel(modelId);
  },
};
