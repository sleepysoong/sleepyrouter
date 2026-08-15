import type { ModelGroups } from "../types.js";
import { completeGroupOrder } from "../types.js";

export function normalizeModelGroupName(value: string): string {
  if (!value) return "";
  return value.toLowerCase().trim();
}

export function normalizeModelGroupsOrdered(value: unknown): {
  groups: ModelGroups;
  order: string[];
} {
  const groups: ModelGroups = {};
  const order: string[] = [];

  if (value && typeof value === "object" && !Array.isArray(value)) {
    for (const [key, raw] of Object.entries(value as Record<string, unknown>)) {
      const ids = stringsFromUnknownSlice(raw);
      if (!ids) continue;
      groups[key] = ids;
      order.push(key);
    }
  }
  order.sort();
  return { groups, order };
}

function stringsFromUnknownSlice(value: unknown): string[] | null {
  if (!Array.isArray(value)) return null;
  return value.filter((v): v is string => typeof v === "string");
}

export function allGroupModelIDs(
  groups: ModelGroups,
  ...groupOrder: string[]
): string[] {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const group of completeGroupOrder(groups, groupOrder)) {
    for (const id of groups[group] ?? []) {
      if (!seen.has(id)) {
        seen.add(id);
        result.push(id);
      }
    }
  }
  return result;
}

export function resolveDefaultGroup(
  groups: ModelGroups,
  defaultGroup: string | undefined,
  ...groupOrder: string[]
): string {
  if (defaultGroup && defaultGroup in groups) return defaultGroup;
  const order = completeGroupOrder(groups, groupOrder);
  return order[0] ?? "";
}
