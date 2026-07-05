export type ModelSource = 'openrouter' | 'nvidia' | 'copilot';

export type ModelGroups = Record<string, string[]>;

export function sourceOf(model: OmfmModel): ModelSource {
  if (model.source === 'nvidia') return 'nvidia';
  if (model.source === 'copilot') return 'copilot';
  return 'openrouter';
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

export interface UsageLogEntry {
  ts: string;
  model: string;
  inputTokens: number;
  outputTokens: number;
  success: boolean;
}

export interface OmfmConfig {
  port: number;
  modelGroups: ModelGroups;
  defaultGroup?: string;
}

export interface ModelCache {
  models: OmfmModel[];
  fetchedAt: string;
}

export type FetchLike = typeof fetch;

export type ProviderApiKeys = Partial<Record<ModelSource, string>>;
