import { ConfigStore, isModelCacheFresh } from '../config/store.js';
import { FetchLike, OmfmModel, ProviderApiKeys, sourceOf } from '../types.js';
import { listCopilotFreeModels } from './copilot.js';
import { listNvidiaFreeModels } from './nvidia.js';
import { listOpenRouterFreeModels } from './openrouter.js';

export interface ProviderCatalogResult {
  models: OmfmModel[];
  errors: string[];
}

export interface LoadedModelCatalog {
  models: OmfmModel[];
  source: 'fresh' | 'fetched' | 'stale';
  errors: string[];
}

function errorMessage(source: string, error: unknown): string {
  return `${source}: ${error instanceof Error ? error.message : String(error)}`;
}

function modelsForConfiguredProviders(models: OmfmModel[], apiKeys: ProviderApiKeys): OmfmModel[] {
  return uniqueModelsById(models.filter((model) => Boolean(apiKeys[sourceOf(model)])));
}

function compareByPopularity(a: OmfmModel, b: OmfmModel): number {
  return (a.popularityRank ?? Number.MAX_SAFE_INTEGER) - (b.popularityRank ?? Number.MAX_SAFE_INTEGER)
    || (a.source ?? 'openrouter').localeCompare(b.source ?? 'openrouter')
    || a.provider.localeCompare(b.provider)
    || a.name.localeCompare(b.name)
    || a.id.localeCompare(b.id);
}

function uniqueModelsById(models: OmfmModel[]): OmfmModel[] {
  const byId = new Map<string, OmfmModel>();
  for (const model of models) {
    if (!byId.has(model.id)) byId.set(model.id, model);
  }
  return [...byId.values()];
}

export async function listAvailableFreeModels(options: { apiKeys: ProviderApiKeys; fetchImpl?: FetchLike }): Promise<ProviderCatalogResult> {
  const tasks: Array<Promise<OmfmModel[]>> = [];
  const labels: string[] = [];
  if (options.apiKeys.openrouter) {
    labels.push('OpenRouter');
    tasks.push(listOpenRouterFreeModels({ apiKey: options.apiKeys.openrouter, fetchImpl: options.fetchImpl }));
  }
  if (options.apiKeys.nvidia) {
    labels.push('NVIDIA');
    tasks.push(listNvidiaFreeModels({ apiKey: options.apiKeys.nvidia, fetchImpl: options.fetchImpl }));
  }
  if (options.apiKeys.copilot) {
    labels.push('Copilot');
    tasks.push(listCopilotFreeModels({ apiKey: options.apiKeys.copilot, fetchImpl: options.fetchImpl }));
  }

  const start = Date.now();
  const settled = await Promise.allSettled(tasks);
  const elapsed = Date.now() - start;
  const models: OmfmModel[] = [];
  const errors: string[] = [];
  for (const [index, result] of settled.entries()) {
    if (result.status === 'fulfilled') models.push(...result.value);
    else errors.push(errorMessage(labels[index] ?? 'provider', result.reason));
  }

  return {
    models: uniqueModelsById(models.sort(compareByPopularity)),
    errors,
  };
}

export async function loadModelCatalog(options: { apiKeys: ProviderApiKeys; fetchImpl?: FetchLike; store: ConfigStore }): Promise<LoadedModelCatalog> {
  const cache = options.store.readModelCache();
  const cachedModels = cache ? modelsForConfiguredProviders(cache.models, options.apiKeys) : [];
  if (cache && isModelCacheFresh(cache) && cachedModels.length > 0) return { models: cachedModels, source: 'fresh', errors: [] };

  const start = Date.now();
  const result = await listAvailableFreeModels({ apiKeys: options.apiKeys, fetchImpl: options.fetchImpl });
  const elapsed = Date.now() - start;
  if (result.models.length > 0) {
    options.store.writeModelCache({ models: result.models, fetchedAt: new Date().toISOString() });
    return { models: result.models, source: 'fetched', errors: result.errors };
  }
  if (cache && cachedModels.length > 0) return { models: cachedModels, source: 'stale', errors: result.errors };
  throw new Error(result.errors.length > 0 ? `모든 프로바이더 모델 가져오기 실패 (${elapsed}ms): ${result.errors.join('; ')}` : '사용 가능한 프로바이더 모델이 없어요.');
}
