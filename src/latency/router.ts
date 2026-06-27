import { selectedGroupModelIds, resolveDefaultGroup } from '../model-groups.js';
import { ModelGroups } from '../types.js';

const GENERIC_MODELS = new Set(['', 'auto', 'default', 'slr', 'openrouter/free']);

export interface RouteChoice {
  modelId: string;
  reason: 'requested-selected' | 'model-group' | 'fallback-order';
}

export function chooseModel(selectedModelIds: string[], requestedModel?: string): RouteChoice {
  if (selectedModelIds.length === 0) {
    throw new Error('선택된 모델이 없어요. config.json에서 모델을 하나 이상 선택하세요.');
  }
  if (requestedModel && !GENERIC_MODELS.has(requestedModel) && selectedModelIds.includes(requestedModel)) {
    return { modelId: requestedModel, reason: 'requested-selected' };
  }
  return { modelId: selectedModelIds[0]!, reason: 'fallback-order' };
}

function candidatePool(selectedModelIds: string[], requestedModel?: string, modelGroups?: ModelGroups, defaultGroup?: string): { ids: string[]; grouped: boolean } {
  if (requestedModel && !GENERIC_MODELS.has(requestedModel) && selectedModelIds.includes(requestedModel)) {
    return { ids: selectedModelIds, grouped: false };
  }

  const groupIds = modelGroups ? selectedGroupModelIds(selectedModelIds, modelGroups, requestedModel) : undefined;
  if (groupIds) return { ids: groupIds, grouped: true };

  if (modelGroups) {
    const resolvedDefault = resolveDefaultGroup(modelGroups, defaultGroup);
    if (resolvedDefault) {
      const defaultIds = selectedGroupModelIds(selectedModelIds, modelGroups, resolvedDefault);
      if (defaultIds) return { ids: defaultIds, grouped: true };
    }
  }

  return { ids: selectedModelIds, grouped: false };
}

export function chooseGroupedModel(selectedModelIds: string[], requestedModel?: string, modelGroups?: ModelGroups, defaultGroup?: string): RouteChoice {
  if (requestedModel && !GENERIC_MODELS.has(requestedModel) && selectedModelIds.includes(requestedModel)) {
    return { modelId: requestedModel, reason: 'requested-selected' };
  }
  const pool = candidatePool(selectedModelIds, requestedModel, modelGroups, defaultGroup);
  const choice = chooseModel(pool.ids, requestedModel);
  return pool.grouped && choice.reason !== 'requested-selected' ? { ...choice, reason: 'model-group' } : choice;
}

export function orderedCandidates(selectedModelIds: string[], requestedModel?: string, modelGroups?: ModelGroups, defaultGroup?: string): string[] {
  const pool = candidatePool(selectedModelIds, requestedModel, modelGroups, defaultGroup);
  const first = chooseGroupedModel(selectedModelIds, requestedModel, modelGroups, defaultGroup).modelId;
  const rest = pool.ids.filter((id) => id !== first);
  return [first, ...rest];
}
