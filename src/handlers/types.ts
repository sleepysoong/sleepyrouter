import type { SleepyRouterModel, ModelGroups } from "../types.js";
import type { ConfigStore } from "../config/index.js";

export interface ServerLogEvent {
  type: string;
  id: number;
  method: string;
  path: string;
  statusCode?: number;
  durationMs?: number;
  requestedModel?: string;
  modelId?: string;
  routeReason?: string;
  stream?: boolean;
  inputTokens?: number;
  outputTokens?: number;
  error?: string;
  group?: string;
  candidateCount?: number;
  triedCount?: number;
}

export interface SelectedModelsResult {
  models: SleepyRouterModel[];
  byId: Record<string, SleepyRouterModel>;
  ids: string[];
  modelGroups: ModelGroups;
  groupOrder: string[];
  defaultModelGroup?: string;
}

export interface HandlerDeps {
  store: ConfigStore;
  env: Record<string, string | undefined>;
  requestLogger?: (event: ServerLogEvent) => void;
}

export type ApiType = "openai" | "anthropic";
