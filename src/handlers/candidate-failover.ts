import type { ProviderAPIKeys } from "../types.js";
import type { SelectedModelsResult, HandlerDeps, ApiType } from "./types.js";
import type { RouteReason } from "../routing/index.js";
import { defaultExecutionPipeline } from "../pipeline/index.js";

export async function tryModelCandidates(
  deps: HandlerDeps,
  apiKeys: ProviderAPIKeys,
  selected: SelectedModelsResult,
  candidates: string[],
  _candidateReason: RouteReason,
  body: Record<string, unknown>,
  isStream: boolean,
  apiType: ApiType,
  _requestId: number,
): Promise<Response> {
  return defaultExecutionPipeline.processCandidates(
    deps,
    apiKeys,
    selected,
    candidates,
    body,
    isStream,
    apiType,
  );
}
