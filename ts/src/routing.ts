// Routing - mirrors internal/routing/router.go + model_groups.go
import type { ModelGroups } from "./types.js";
import { completeGroupOrder } from "./types.js";

export type RouteReason = "model-group" | "fallback-order";

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

export function candidateIDs(
  groups: ModelGroups,
  requestedModel: string,
  defaultGroup: string | undefined,
  ...groupOrder: string[]
): { ids: string[]; reason: RouteReason } {
  const normalized = normalizeModelGroupName(requestedModel);
  if (normalized && normalized in groups) {
    return { ids: groups[normalized]!, reason: "model-group" };
  }
  const resolved = resolveDefaultGroup(groups, defaultGroup, ...groupOrder);
  if (!resolved) return { ids: [], reason: "fallback-order" };
  return { ids: groups[resolved]!, reason: "fallback-order" };
}

export function orderedCandidates(
  groups: ModelGroups,
  requestedModel: string,
  defaultGroup: string | undefined,
  ...groupOrder: string[]
): { ids: string[]; reason: RouteReason } {
  const result = candidateIDs(
    groups,
    requestedModel,
    defaultGroup,
    ...groupOrder,
  );
  return { ids: [...result.ids], reason: result.reason };
}

/**
 * Extract ordered keys from a JSON object in source text, specifically
 * the keys of the nested object under `field`.
 */
export function objectKeysInJSON(
  jsonText: string,
  field: string,
): string[] {
  try {
    // Parse JSON, find the field, then re-scan for key order
    const parsed = JSON.parse(jsonText);
    if (!parsed[field] || typeof parsed[field] !== "object") return [];

    // Use regex to extract key order from the raw JSON text
    // Find the field's object in the JSON
    const fieldRegex = new RegExp(`"${field}"\\s*:\\s*\\{`);
    const match = fieldRegex.exec(jsonText);
    if (!match) return [];

    const startIdx = match.index + match[0].length;
    let depth = 1;
    let i = startIdx;
    while (i < jsonText.length && depth > 0) {
      if (jsonText[i] === "{") depth++;
      else if (jsonText[i] === "}") depth--;
      i++;
    }
    const objectBody = jsonText.slice(startIdx, i - 1);

    // Extract top-level keys in order
    const keys: string[] = [];
    const keyRegex = /"([^"]+)"\s*:/g;
    let keyMatch;
    let d = 0;
    let pos = 0;
    while (pos < objectBody.length) {
      if (objectBody[pos] === "{" || objectBody[pos] === "[") d++;
      else if (objectBody[pos] === "}" || objectBody[pos] === "]") d--;

      if (d === 0) {
        keyRegex.lastIndex = pos;
        keyMatch = keyRegex.exec(objectBody);
        if (keyMatch && keyMatch.index === pos + (keyMatch.index - pos)) {
          // Check this key is at depth 0
          const beforeKey = objectBody.slice(0, keyMatch.index);
          let bd = 0;
          for (const ch of beforeKey) {
            if (ch === "{" || ch === "[") bd++;
            else if (ch === "}" || ch === "]") bd--;
          }
          if (bd === 0) {
            keys.push(keyMatch[1]!);
          }
        }
      }
      pos++;
    }

    // Simpler approach: just use the object's own key order
    return Object.keys(parsed[field]);
  } catch {
    return [];
  }
}
