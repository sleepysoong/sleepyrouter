import type { SleepyRouterModel, ProviderAPIKeys } from "../types.js";
import type { SelectedModelsResult, HandlerDeps, ApiType } from "../handlers/types.js";
import { defaultRequestPipeline, RequestPipeline } from "./request-pipeline.js";
import { defaultBaseProviderPipeline, BaseProviderPipeline } from "./provider-pipeline.js";
import { defaultResultAdapterRegistry, ResultAdapterRegistry } from "./result-adapter.js";
import { truncate } from "../utils.js";

export class ExecutionPipeline {
  constructor(
    private requestPipeline: RequestPipeline = defaultRequestPipeline,
    private providerPipeline: BaseProviderPipeline = defaultBaseProviderPipeline,
    private resultRegistry: ResultAdapterRegistry = defaultResultAdapterRegistry,
  ) {}

  async processCandidates(
    deps: HandlerDeps,
    apiKeys: ProviderAPIKeys,
    selected: SelectedModelsResult,
    candidates: string[],
    body: Record<string, unknown>,
    isStream: boolean,
    apiType: ApiType,
  ): Promise<Response> {
    let upstreamError = "";
    let triedAny = false;

    const resultAdapter = this.resultRegistry.get(apiType);

    for (const modelID of candidates) {
      const model = selected.byId[modelID];
      if (!model) continue;

      triedAny = true;
      const upstreamModelID = model.upstreamId || model.id;

      try {
        // Stage 1: Request Pipeline Layer
        const normalizedParams = this.requestPipeline.normalize(
          body,
          upstreamModelID,
          model.provider,
          apiType,
        );

        // Stage 2 & 3: Base Provider Layer & Concrete Provider Execution
        const execResult = await this.providerPipeline.execute(
          model,
          apiKeys,
          normalizedParams,
          isStream,
        );

        // Usage Logging (Fire and forget for streams, immediate for generate)
        if (execResult.isStream && execResult.streamResult) {
          Promise.resolve(execResult.streamResult.usage)
            .then((usage) => {
              deps.store.appendUsage({
                ts: new Date().toISOString(),
                model: model.usageId || model.id,
                inputTokens: usage.inputTokens ?? 0,
                outputTokens: usage.outputTokens ?? 0,
                success: true,
              });
            })
            .catch(() => {});
        } else if (execResult.generateResult) {
          deps.store.appendUsage({
            ts: new Date().toISOString(),
            model: model.usageId || model.id,
            inputTokens: execResult.generateResult.usage.inputTokens ?? 0,
            outputTokens: execResult.generateResult.usage.outputTokens ?? 0,
            success: true,
          });
        }

        // Stage 4: Result Layer Adapter
        if (execResult.isStream) {
          return resultAdapter.adaptStreamResult(execResult);
        }
        return resultAdapter.adaptGenerateResult(execResult);
      } catch (e) {
        const errMsg = e instanceof Error ? e.message : String(e);
        upstreamError = `[${modelID}] ${truncate(errMsg, 300)}`;
        deps.store.appendUsage({
          ts: new Date().toISOString(),
          model: model.usageId || model.id,
          inputTokens: 0,
          outputTokens: 0,
          success: false,
        });
        continue;
      }
    }

    if (!triedAny) {
      return Response.json(
        {
          error: {
            message:
              "사용 가능한 모델이 없어요. API 키를 확인하세요.",
            details: upstreamError,
          },
        },
        { status: 502 },
      );
    }

    const extras: Record<string, unknown> = { details: upstreamError };
    if (apiType === "anthropic") {
      extras["type"] = "api_error";
    }
    return Response.json(
      { error: { message: "선택된 모든 무료 모델이 실패했어요.", ...extras } },
      { status: 502 },
    );
  }
}

export const defaultExecutionPipeline = new ExecutionPipeline();
