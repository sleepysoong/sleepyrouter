import type { ModelGroups } from "../types.js";
import { normalizeModelGroupName, resolveDefaultGroup } from "./model-groups.js";

export type RouteReason = "model-group" | "fallback-order";

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
