import { generateText, streamText } from "ai";
import type { SleepyRouterModel, ProviderAPIKeys } from "../types.js";
import { sourceOf } from "../types.js";
import type { SelectedModelsResult, HandlerDeps, ApiType } from "./types.js";
import { apiKeyFor } from "../config/index.js";
import { getProvider, type ProviderAdapter } from "../providers/index.js";
import { defaultProtocolTransformerRegistry } from "../protocol/index.js";
import { truncate } from "../utils.js";
import { buildOpenAIResponse, buildAnthropicResponse } from "./response-builder.js";
import { streamOpenAIAsAnthropic } from "./stream-converter.js";
import type { RouteReason } from "../routing/index.js";

function missingKeyMessage(model: SleepyRouterModel, provider?: ProviderAdapter): string {
  const keyName = provider?.apiKeyEnvVar ?? "OPENROUTER_API_KEY";
  return `${keyName}가 없어서 ${model.id}을(를) 사용할 수 없어요. 환경변수 또는 .env 파일에 키를 추가하세요.`;
}

function modelUpstreamID(model: SleepyRouterModel): string {
  return model.upstreamId || model.id;
}

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
  let upstreamError = "";
  let triedAny = false;
  let _triedCount = 0;

  const transformer = defaultProtocolTransformerRegistry.get(apiType);

  for (const modelID of candidates) {
    const model = selected.byId[modelID];
    if (!model) continue;

    const source = sourceOf(model);
    const p = getProvider(source);
    if (!p) {
      upstreamError = `unsupported provider: ${source}`;
      continue;
    }

    let apiKey = apiKeyFor(apiKeys, source);
    if (!apiKey) {
      upstreamError = missingKeyMessage(model, p);
      continue;
    }

    if (p.prepareApiKey) {
      try {
        apiKey = await p.prepareApiKey(apiKey);
      } catch (e) {
        upstreamError = e instanceof Error ? e.message : String(e);
        continue;
      }
    }

    triedAny = true;
    _triedCount++;

    const upstreamModelID = modelUpstreamID(model);

    try {
      const requestBody = transformer.transformRequest(body, upstreamModelID, p.name);
      const langModel = p.chatModel(upstreamModelID, apiKey);

      if (isStream) {
        const result = await streamText({
          model: langModel,
          maxRetries: 0,
          messages: requestBody["messages"] as any,
          system: requestBody["system"] as string | undefined,
          temperature: requestBody["temperature"] as number | undefined,
          topP: requestBody["top_p"] as number | undefined,
          maxOutputTokens: (requestBody["max_tokens"] as number) || undefined,
          tools: undefined,
          providerOptions: {
            [p.name.toLowerCase()]: requestBody as any,
          } as any,
        });

        Promise.resolve(result.usage)
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

        if (apiType === "anthropic") {
          return streamOpenAIAsAnthropic(result, upstreamModelID);
        }

        return result.toTextStreamResponse();
      }

      const result = await generateText({
        model: langModel,
        maxRetries: 0,
        messages: requestBody["messages"] as any,
        system: requestBody["system"] as string | undefined,
        temperature: requestBody["temperature"] as number | undefined,
        topP: requestBody["top_p"] as number | undefined,
        maxOutputTokens: (requestBody["max_tokens"] as number) || undefined,
        providerOptions: {
          [p.name.toLowerCase()]: requestBody as any,
        } as any,
      });

      deps.store.appendUsage({
        ts: new Date().toISOString(),
        model: model.usageId || model.id,
        inputTokens: result.usage.inputTokens ?? 0,
        outputTokens: result.usage.outputTokens ?? 0,
        success: true,
      });

      if (apiType === "anthropic") {
        const anthropicResponse = buildAnthropicResponse(result, upstreamModelID);
        return Response.json(anthropicResponse);
      }

      const openaiResponse = buildOpenAIResponse(result, upstreamModelID);
      return Response.json(openaiResponse);
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

  const extras: Record<string, unknown> = {
    details: upstreamError,
  };
  if (apiType === "anthropic") {
    extras["type"] = "api_error";
  }
  return Response.json(
    { error: { message: "선택된 모든 무료 모델이 실패했어요.", ...extras } },
    { status: 502 },
  );
}
