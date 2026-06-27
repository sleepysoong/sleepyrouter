import { ModelGroups } from './types.js';

export const DEFAULT_MODEL_GROUPS: ModelGroups = {};

const LEGACY_ALIASES: Record<string, string> = {
  haiku: 'fast',
  sonnet: 'balanced',
  opus: 'capable',
};

export function normalizeModelGroupName(value: string | undefined): string | undefined {
  if (!value) return undefined;
  const normalized = value.trim().toLowerCase().replace(/^slr\//, '');
  return LEGACY_ALIASES[normalized] ?? normalized;
}

export function normalizeModelGroups(value: unknown): ModelGroups {
  if (!value || typeof value !== 'object') return {};
  const source = value as Record<string, unknown>;
  const result: ModelGroups = {};
  for (const [key, val] of Object.entries(source)) {
    if (Array.isArray(val)) {
      result[key] = val.filter((x): x is string => typeof x === 'string');
    }
  }
  return result;
}

export function selectedGroupModelIds(selectedModelIds: string[], modelGroups: ModelGroups, requestedModel?: string): string[] | undefined {
  const group = normalizeModelGroupName(requestedModel);
  if (!group) return undefined;
  const ids = modelGroups[group];
  if (!ids || ids.length === 0) return undefined;
  const selected = new Set(selectedModelIds);
  const filtered = ids.filter((id) => selected.has(id));
  return filtered.length > 0 ? filtered : undefined;
}

export function resolveDefaultGroup(modelGroups: ModelGroups, defaultGroup?: string): string | undefined {
  if (defaultGroup && modelGroups[defaultGroup]) return defaultGroup;
  const keys = Object.keys(modelGroups);
  return keys.length > 0 ? keys[0] : undefined;
}
