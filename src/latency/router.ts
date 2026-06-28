import { normalizeModelGroupName, resolveDefaultGroup } from '../model-groups.js';
import { ModelGroups } from '../types.js';

// ponytail: 그룹명으로만 라우팅 — 모델ID 매칭 없음
// 요청된 이름이 그룹명이면 해당 그룹, 아니면 defaultGroup

export interface RouteChoice {
  modelId: string;
  reason: 'model-group' | 'fallback-order';
}

function candidateIds(modelGroups: ModelGroups, requestedModel?: string, defaultGroup?: string): string[] {
  const normalized = normalizeModelGroupName(requestedModel);
  if (normalized && modelGroups[normalized]) return modelGroups[normalized]!;
  const resolved = resolveDefaultGroup(modelGroups, defaultGroup);
  if (resolved) return modelGroups[resolved]!;
  return [];
}

export function chooseModel(modelGroups: ModelGroups, requestedModel?: string): RouteChoice {
  const ids = candidateIds(modelGroups, requestedModel);
  if (ids.length === 0) throw new Error('선택된 모델이 없어요. config.json에서 모델을 하나 이상 선택하세요.');
  const normalized = normalizeModelGroupName(requestedModel);
  const reason = normalized && modelGroups[normalized] ? 'model-group' : 'fallback-order';
  return { modelId: ids[0]!, reason };
}

export function chooseGroupedModel(modelGroups: ModelGroups, requestedModel?: string, defaultGroup?: string): RouteChoice {
  const ids = candidateIds(modelGroups, requestedModel, defaultGroup);
  if (ids.length === 0) throw new Error('선택된 모델이 없어요. config.json에서 모델을 하나 이상 선택하세요.');
  const normalized = normalizeModelGroupName(requestedModel);
  const reason = normalized && modelGroups[normalized] ? 'model-group' : 'fallback-order';
  return { modelId: ids[0]!, reason };
}

export function orderedCandidates(modelGroups: ModelGroups, requestedModel?: string, defaultGroup?: string): string[] {
  return candidateIds(modelGroups, requestedModel, defaultGroup);
}
