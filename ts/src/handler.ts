// Handler - mirrors internal/handler/ + internal/srv/
import { streamText, generateText, type LanguageModel } from "ai";
import type {
  SleepyRouterModel,
  ProviderAPIKeys,
  ModelGroups,
} from "./types.js";
import { sourceOf, apiKeyFor } from "./types.js";
import { ConfigStore, requireAnyProviderAPIKey } from "./config.js";
import {
  allGroupModelIDs,
  orderedCandidates,
  type RouteReason,
} from "./routing.js";
import {
  getProvider,
  copilotSessionToken,
  type Provider,
} from "./providers.js";
import {
  anthropicToOpenAI,
  openAIToAnthropic,
  estimateInputTokens,
  mapStopReason,
} from "./protocol.js";
import { stringFromUnknown, boolValue, truncate, safeLogValue } from "./utils.js";

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

export function selectedModelSelection(
  store: ConfigStore,
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

function missingKeyMessage(model: SleepyRouterModel): string {
  const source = sourceOf(model);
  let keyName = "OPENROUTER_API_KEY";
  switch (source) {
    case "nvidia":
      keyName = "NVIDIA_API_KEY";
      break;
    case "copilot":
      keyName = "GITHUB_COPILOT_TOKEN";
      break;
    case "zen":
      keyName = "OPENCODE_API_KEY";
      break;
    case "google":
      keyName = "GOOGLE_API_KEY";
      break;
  }
  return `${keyName}가 없어서 ${model.id}을(를) 사용할 수 없어요. 환경변수 또는 .env 파일에 키를 추가하세요.`;
}

function modelUpstreamID(model: SleepyRouterModel): string {
  return model.upstreamId || model.id;
}

// OpenAI allow-list fields
const OPENAI_CHAT_COMPLETIONS_FIELDS = new Set([
  "messages",
  "model",
  "frequency_penalty",
  "max_tokens",
  "n",
  "presence_penalty",
  "response_format",
  "seed",
  "stop",
  "stream",
  "temperature",
  "top_p",
  "tools",
  "tool_choice",
  "parallel_tool_calls",
  "user",
  "logprobs",
  "top_logprobs",
  "reasoning_effort",
  "reasoning",
  "stream_options",
]);

export interface HandlerDeps {
  store: ConfigStore;
  env: Record<string, string | undefined>;
  requestLogger?: (event: ServerLogEvent) => void;
}

type ApiType = "openai" | "anthropic";

async function tryModelCandidates(
  deps: HandlerDeps,
  apiKeys: ProviderAPIKeys,
  selected: SelectedModelsResult,
  candidates: string[],
  candidateReason: RouteReason,
  body: Record<string, unknown>,
  isStream: boolean,
  apiType: ApiType,
  requestId: number,
): Promise<Response> {
  let upstreamError = "";
  let triedAny = false;
  let triedCount = 0;

  for (const modelID of candidates) {
    const model = selected.byId[modelID];
    if (!model) continue;

    const source = sourceOf(model);
    let apiKey = apiKeyFor(apiKeys, source);
    if (!apiKey) {
      upstreamError = missingKeyMessage(model);
      continue;
    }

    // For Copilot, exchange PAT for session token
    if (source === "copilot") {
      try {
        apiKey = await copilotSessionToken(apiKey);
      } catch (e) {
        upstreamError = e instanceof Error ? e.message : String(e);
        continue;
      }
    }

    const p = getProvider(source);
    if (!p) {
      upstreamError = `unsupported provider: ${source}`;
      continue;
    }

    triedAny = true;
    triedCount++;

    const upstreamModelID = modelUpstreamID(model);

    try {
      // Build the upstream request body
      let requestBody: Record<string, unknown>;
      if (apiType === "anthropic") {
        // Convert Anthropic request to OpenAI format for upstream
        requestBody = anthropicToOpenAI(body, upstreamModelID, p.name);
      } else {
        // Filter to allowed OpenAI fields
        requestBody = {};
        for (const [key, value] of Object.entries(body)) {
          if (OPENAI_CHAT_COMPLETIONS_FIELDS.has(key)) {
            requestBody[key] = value;
          }
        }
        requestBody["model"] = upstreamModelID;
      }

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
          tools: undefined, // Tool handling via raw body passthrough
          providerOptions: {
            [p.name.toLowerCase()]: requestBody as any,
          } as any,
        });

        // Record usage after stream completes (fire and forget)
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
          .catch(() => {}); // best effort

        if (apiType === "anthropic") {
          // Convert OpenAI stream to Anthropic SSE format
          return streamOpenAIAsAnthropic(result, upstreamModelID);
        }

        // OpenAI passthrough stream
        return result.toTextStreamResponse();
      }

      // Non-streaming
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

      // Record usage
      deps.store.appendUsage({
        ts: new Date().toISOString(),
        model: model.usageId || model.id,
        inputTokens: result.usage.inputTokens ?? 0,
        outputTokens: result.usage.outputTokens ?? 0,
        success: true,
      });

      if (apiType === "anthropic") {
        // Build Anthropic-format response from the raw response
        const anthropicResponse = buildAnthropicResponse(result, upstreamModelID);
        return Response.json(anthropicResponse);
      }

      // OpenAI response - build from generateText result
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

function buildOpenAIResponse(
  result: Awaited<ReturnType<typeof generateText>>,
  model: string,
): Record<string, unknown> {
  const message: Record<string, unknown> = {
    role: "assistant",
    content: result.text || null,
  };
  if (result.reasoning) {
    message["reasoning_content"] = result.reasoning;
  }
  if (result.toolCalls && result.toolCalls.length > 0) {
    message["tool_calls"] = result.toolCalls.map((tc, i) => ({
      index: i,
      id: tc.toolCallId,
      type: "function",
      function: {
        name: tc.toolName,
        arguments: JSON.stringify((tc as any).input ?? (tc as any).args ?? {}),
      },
    }));
  }

  const inputTokens = result.usage.inputTokens ?? 0;
  const outputTokens = result.usage.outputTokens ?? 0;

  return {
    id: `chatcmpl-${Date.now()}`,
    object: "chat.completion",
    created: Math.floor(Date.now() / 1000),
    model,
    choices: [
      {
        index: 0,
        message,
        finish_reason: result.finishReason ?? "stop",
      },
    ],
    usage: {
      prompt_tokens: inputTokens,
      completion_tokens: outputTokens,
      total_tokens: inputTokens + outputTokens,
    },
  };
}

function buildAnthropicResponse(
  result: Awaited<ReturnType<typeof generateText>>,
  model: string,
): Record<string, unknown> {
  const blocks: Record<string, unknown>[] = [];

  if (result.reasoning) {
    blocks.push({
      type: "thinking",
      thinking: result.reasoning,
    });
  }
  if (result.text) {
    blocks.push({ type: "text", text: result.text });
  }
  if (result.toolCalls) {
    for (const tc of result.toolCalls) {
      blocks.push({
        type: "tool_use",
        id: tc.toolCallId,
        name: tc.toolName,
        input: (tc as any).input ?? (tc as any).args ?? {},
      });
    }
  }

  let stopReason = "end_turn";
  if (result.finishReason === "length") stopReason = "max_tokens";
  else if (result.finishReason === "tool-calls") stopReason = "tool_use";

  return {
    id: `msg_${Date.now()}`,
    type: "message",
    role: "assistant",
    content: blocks,
    model,
    stop_reason: stopReason,
    stop_sequence: null,
    usage: {
      input_tokens: result.usage.inputTokens ?? 0,
      output_tokens: result.usage.outputTokens ?? 0,
    },
  };
}

function streamOpenAIAsAnthropic(
  result: Awaited<ReturnType<typeof streamText>>,
  model: string,
): Response {
  const encoder = new TextEncoder();

  const stream = new ReadableStream({
    async start(controller) {
      function writeSSE(event: string, data: unknown) {
        const json = JSON.stringify(data);
        controller.enqueue(
          encoder.encode(`event: ${event}\ndata: ${json}\n\n`),
        );
      }

      // message_start
      writeSSE("message_start", {
        type: "message_start",
        message: {
          id: `msg_${Date.now()}`,
          type: "message",
          role: "assistant",
          content: [],
          model,
          stop_reason: null,
          stop_sequence: null,
          usage: { input_tokens: 0, output_tokens: 0 },
        },
      });

      let blockIndex = 0;
      let textBlockStarted = false;

      try {
        for await (const chunk of result.textStream) {
          if (!textBlockStarted) {
            writeSSE("content_block_start", {
              type: "content_block_start",
              index: blockIndex,
              content_block: { type: "text", text: "" },
            });
            textBlockStarted = true;
          }
          writeSSE("content_block_delta", {
            type: "content_block_delta",
            index: blockIndex,
            delta: { type: "text_delta", text: chunk },
          });
        }

        if (textBlockStarted) {
          writeSSE("content_block_stop", {
            type: "content_block_stop",
            index: blockIndex,
          });
        }
      } catch {
        // Stream error - still send message_stop
      }

      // message_delta with usage
      const usage = await Promise.resolve(result.usage);
      writeSSE("message_delta", {
        type: "message_delta",
        delta: { stop_reason: "end_turn", stop_sequence: null },
        usage: { output_tokens: usage.outputTokens ?? 0 },
      });

      writeSSE("message_stop", { type: "message_stop" });
      controller.close();
    },
  });

  return new Response(stream, {
    headers: {
      "Content-Type": "text/event-stream; charset=utf-8",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
    },
  });
}

// ---- Route handlers ----

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
