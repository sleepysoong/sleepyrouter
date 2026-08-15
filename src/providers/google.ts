import { createGoogle } from "@ai-sdk/google";
import type { Provider } from "./base.js";

export const googleProvider: Provider = {
  name: "Google",
  source: "google",
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
