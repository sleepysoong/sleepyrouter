import type { LanguageModel } from "ai";
import type { ModelSource } from "../types.js";

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

export function baseURLFrom(envVar: string, def: string): string {
  return process.env[envVar] || def;
}
