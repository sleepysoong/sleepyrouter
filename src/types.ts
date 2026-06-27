export type ModelSource = 'openrouter' | 'nvidia';

export type ModelGroupName = 'fast' | 'balanced' | 'capable';

export type ModelGroups = Record<ModelGroupName, string[]>;

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
}

export interface ModelCache {
  models: OmfmModel[];
  fetchedAt: string;
}

export type FetchLike = typeof fetch;

export type ProviderApiKeys = Partial<Record<ModelSource, string>>;
