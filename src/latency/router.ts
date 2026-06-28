import { allGroupModelIds, selectedGroupModelIds, resolveDefaultGroup } from '../model-groups.js';
import { ModelGroups } from '../types.js';

const GENERIC_MODELS = new Set(['', 'auto', 'default', 'slr', 'openrouter/free']);

export interface RouteChoice {
  modelId: string;
  reason: 'requested-selected' | 'model-group' | 'fallback-order';
}

function allModels(modelGroups: ModelGroups): string[] {
  return allGroupModelIds(modelGroups);
}

function isGeneric(model: string | undefined): boolean {
  return !model || GENERIC_MODELS.has(model);
}

export function chooseModel(modelGroups: ModelGroups, requestedModel?: string): RouteChoice {
  const ids = allModels(modelGroups);
  if (ids.length === 0) {
    throw new Error('선택된 모델이 없어요. config.json에서 모델을 하나 이상 선택하세요.');
  }
  if (!isGeneric(requestedModel) && ids.includes(requestedModel!)) {
    return { modelId: requestedModel!, reason: 'requested-selected' };
  }
  return { modelId: ids[0]!, reason: 'fallback-order' };
}

function resolveCandidateIds(modelGroups: ModelGroups, requestedModel?: string, defaultGroup?: string): { ids: string[]; grouped: boolean } {
  const allIds = allModels(modelGroups);

  if (!isGeneric(requestedModel) && allIds.includes(requestedModel!)) {
    return { ids: allIds, grouped: false };
  }

  const groupIds = selectedGroupModelIds(modelGroups, requestedModel);
  if (groupIds) return { ids: groupIds, grouped: true };

  const resolvedDefault = resolveDefaultGroup(modelGroups, defaultGroup);
  if (resolvedDefault) {
    const defaultIds = selectedGroupModelIds(modelGroups, resolvedDefault);
    if (defaultIds) return { ids: defaultIds, grouped: true };
  }

  return { ids: allIds, grouped: false };
}

export function chooseGroupedModel(modelGroups: ModelGroups, requestedModel?: string, defaultGroup?: string): RouteChoice {
  if (!isGeneric(requestedModel) && allModels(modelGroups).includes(requestedModel!)) {
    return { modelId: requestedModel!, reason: 'requested-selected' };
  }
  const pool = resolveCandidateIds(modelGroups, requestedModel, defaultGroup);
  if (pool.ids.length === 0) {
    throw new Error('선택된 모델이 없어요. config.json에서 모델을 하나 이상 선택하세요.');
  }
  const first = pool.ids[0]!;
  return pool.grouped ? { modelId: first, reason: 'model-group' } : { modelId: first, reason: 'fallback-order' };
}

export function orderedCandidates(modelGroups: ModelGroups, requestedModel?: string, defaultGroup?: string): string[] {
  const pool = resolveCandidateIds(modelGroups, requestedModel, defaultGroup);
  return pool.ids;
}
