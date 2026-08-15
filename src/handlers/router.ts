import type { SleepyRouterModel, ProviderAPIKeys } from "../types.js";
import { sourceOf } from "../types.js";
import type { SelectedModelsResult, HandlerDeps, ApiType } from "./types.js";
import { requireAnyProviderAPIKey } from "../config/index.js";
import {
  allGroupModelIDs,
  orderedCandidates,
} from "../routing/index.js";
import { estimateInputTokens } from "../protocol/index.js";
import { stringFromUnknown, boolValue } from "../utils.js";
import { tryModelCandidates } from "./candidate-failover.js";

export function selectedModelSelection(
  store: HandlerDeps["store"],
  _apiKeys: ProviderAPIKeys,
): SelectedModelsResult {
  const config = store.readConfig();
  const allIDs = allGroupModelIDs(
    config.modelGroups,
    ...(config.groupOrder ?? []),
  );
  const models: SleepyRouterModel[] = [];
  const byId: Record<string, SleepyRouterModel> = {};
  for (const id of allIDs) {
    const def = config.models?.[id];
    if (!def) continue;
    const m: SleepyRouterModel = {
      id,
      upstreamId: def.name,
      provider: def.provider,
      source: def.provider as SleepyRouterModel["source"],
      usageId: id,
    };
    models.push(m);
    byId[id] = m;
  }
  return {
    models,
    byId,
    ids: models.map((m) => m.id),
    modelGroups: config.modelGroups,
    groupOrder: config.groupOrder ?? [],
    defaultModelGroup: config.defaultModelGroup,
  };
}

export function createRouter(deps: HandlerDeps) {
  const { store, env } = deps;

  return {
    async handleHealth(): Promise<Response> {
      return Response.json({
        ok: true,
        service: "sleepyrouter",
        version: "0.0.4",
        uptime: Math.floor(process.uptime()),
      });
    },

    async handleModels(): Promise<Response> {
      const apiKeys = requireAnyProviderAPIKey(env, store.paths.root);
      const selected = selectedModelSelection(store, apiKeys);
      const data = selected.models.map((model) => ({
        id: model.id,
        object: "model",
        created: 0,
        owned_by: sourceOf(model),
        provider: model.provider,
      }));
      return Response.json({ object: "list", data });
    },

    async handleCountTokens(body: Record<string, unknown>): Promise<Response> {
      return Response.json({ input_tokens: estimateInputTokens(body) });
    },

    async handleChat(
      body: Record<string, unknown>,
      apiType: ApiType,
      requestId: number,
    ): Promise<Response> {
      const apiKeys = requireAnyProviderAPIKey(env, store.paths.root);
      const selected = selectedModelSelection(store, apiKeys);

      if (selected.models.length === 0) {
        return Response.json(
          {
            error: {
              message:
                "선택된 무료 모델이 없어요. config.json의 modelGroups에 사용할 모델을 하나 이상 추가하세요.",
            },
          },
          { status: 400 },
        );
      }

      const requestedModel = stringFromUnknown(body["model"]);
      const isStream = boolValue(body["stream"]);

      const { ids: candidates, reason: candidateReason } = orderedCandidates(
        selected.modelGroups,
        requestedModel,
        selected.defaultModelGroup,
        ...selected.groupOrder,
      );

      if (deps.requestLogger) {
        deps.requestLogger({
          type: "route",
          id: requestId,
          method: "POST",
          path: apiType === "anthropic" ? "/anthropic/v1/messages" : "/v1/chat/completions",
          requestedModel,
          candidateCount: candidates.length,
          routeReason: candidateReason,
        });
      }

      return tryModelCandidates(
        deps,
        apiKeys,
        selected,
        candidates,
        candidateReason,
        body,
        isStream,
        apiType,
        requestId,
      );
    },
  };
}
