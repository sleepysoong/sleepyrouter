export type ModelSource = 'openrouter' | 'nvidia';

export type ModelGroups = Record<string, string[]>;

export function sourceOf(model: OmfmModel): ModelSource {
  return model.source === 'nvidia' ? 'nvidia' : 'openrouter';
}

export interface OmfmModel {
  id: string;
  upstreamId?: string;
  name: string;
  provider: string;
  source?: ModelSource;
  usageId?: string;
  contextLength?: number;
  popularityRank?: number;
  supportedParameters?: string[];
  raw?: unknown;
}

export interface UsageObservation {
  modelId: string;
  requests: number;
  successes: number;
  failures: number;
  inputTokens: number;
  outputTokens: number;
  totalTokens: number;
  updatedAt: string;
  lastStatus?: string;
  lastHttpStatus?: number;
}

export interface OmfmConfig {
  port: number;
  selectedModelIds: string[];
  modelGroups: ModelGroups;
  defaultGroup?: string;
}

export interface ModelCache {
  models: OmfmModel[];
  fetchedAt: string;
}

export type FetchLike = typeof fetch;

export type ProviderApiKeys = Partial<Record<ModelSource, string>>;
