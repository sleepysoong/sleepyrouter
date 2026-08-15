import type { LanguageModel } from "ai";
import type { ModelSource } from "../types.js";

export type MessageProtocol = "openai" | "anthropic";

export interface ProviderAdapter {
  readonly name: string;
  readonly source: ModelSource;
  readonly apiKeyEnvVar: string;
  readonly messageProtocol: MessageProtocol;
  prepareApiKey?(apiKey: string): Promise<string> | string;
  chatModel(modelId: string, apiKey: string, customFetch?: typeof fetch): LanguageModel;
}

export type Provider = ProviderAdapter;

export class ProviderRegistry {
  private adapters = new Map<string, ProviderAdapter>();

  register(adapter: ProviderAdapter): this {
    this.adapters.set(adapter.source, adapter);
    return this;
  }

  get(source: ModelSource): ProviderAdapter | undefined {
    return this.adapters.get(source);
  }

  has(source: ModelSource): boolean {
    return this.adapters.has(source);
  }

  getAll(): ProviderAdapter[] {
    return [...this.adapters.values()];
  }

  unregister(source: ModelSource): boolean {
    return this.adapters.delete(source);
  }
}

export const defaultProviderRegistry = new ProviderRegistry();

export function registerProvider(source: ModelSource, adapter: ProviderAdapter): void {
  defaultProviderRegistry.register(adapter);
}

export function getProvider(source: ModelSource): ProviderAdapter | undefined {
  return defaultProviderRegistry.get(source);
}

export function baseURLFrom(envVar: string, def: string): string {
  return process.env[envVar] || def;
}
