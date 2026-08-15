// Types module - mirrors internal/types/types.go

export type ModelSource = "openrouter" | "nvidia" | "copilot" | "zen" | "google";

export type ModelGroups = Record<string, string[]>;

export interface SleepyRouterModel {
  id: string;
  upstreamId?: string;
  provider: string;
  source: ModelSource;
  usageId?: string;
}

export function sourceOf(model: SleepyRouterModel): ModelSource {
  switch (model.source) {
    case "nvidia":
    case "copilot":
    case "zen":
    case "google":
      return model.source;
    default:
      return "openrouter";
  }
}

export interface UsageLogEntry {
  ts: string;
  model: string;
  inputTokens: number;
  outputTokens: number;
  success: boolean;
}

export interface ModelDefinition {
  provider: string;
  name: string;
  inputPrice?: number;
  outputPrice?: number;
}

export interface SleepyRouterConfig {
  port: number;
  modelGroups: ModelGroups;
  defaultModelGroup?: string;
  groupOrder?: string[];
  models?: Record<string, ModelDefinition>;
}

export interface ProviderAPIKeys {
  openRouter: string;
  nvidia: string;
  copilot: string;
  zen: string;
  google: string;
}

export function completeGroupOrder(
  groups: ModelGroups,
  preferred: string[],
): string[] {
  const seen = new Set<string>();
  const order: string[] = [];
  for (const name of preferred) {
    if (!seen.has(name) && name in groups) {
      seen.add(name);
      order.push(name);
    }
  }
  const remaining = Object.keys(groups)
    .filter((n) => !seen.has(n))
    .sort();
  return [...order, ...remaining];
}
