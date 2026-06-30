import http, { IncomingMessage, ServerResponse } from 'node:http';
import { ConfigStore } from '../config/store.js';
import { requireAnyProviderApiKey } from '../config/env.js';
import { loadModelCatalog } from '../providers/catalog.js';
import { postNvidiaChatCompletion } from '../providers/nvidia.js';
import { isFreeOpenRouterModel, postOpenRouterAnthropicMessage, postOpenRouterChatCompletion } from '../providers/openrouter.js';
import { FetchLike, ModelGroups, ModelSource, OmfmModel, ProviderApiKeys, sourceOf } from '../types.js';
import { orderedCandidates, RouteChoice } from '../latency/router.js';
import { allGroupModelIds, normalizeModelGroupName } from '../model-groups.js';
import { anthropicToOpenAI, openAIToAnthropic } from './translate.js';
import { pipeOpenAIStreamAsAnthropic, pipeWebStreamToNode } from './sse.js';
import { VERSION } from '../version.js';

export interface ServerOptions {
  store?: ConfigStore;
  fetchImpl?: FetchLike;
  env?: NodeJS.ProcessEnv;
  requestLogger?: (event: ServerLogEvent) => void;
}

function json(res: ServerResponse, status: number, body: unknown): void {
  res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8' });
  res.end(JSON.stringify(body));
}

export type ServerLogEvent =
  | { type: 'request'; id: number; method: string; path: string }
  | { type: 'response'; id: number; method: string; path: string; statusCode: number; durationMs: number; requestedModel?: string; modelId?: string; routeReason?: RouteChoice['reason'] | 'failover'; stream?: boolean; inputTokens?: number; outputTokens?: number; error?: string; group?: string; triedCount?: number; candidateCount?: number };

interface FormatServerLogEventOptions {
  color?: boolean;
}

function safeLogValue(value: string): string {
  const sanitized = value.replace(/[\u0000-\u001f\u007f]/g, '?');
  return sanitized.length > 200 ? `${sanitized.slice(0, 197)}...` : sanitized;
}

function color(value: string, code: number, enabled: boolean | undefined): string {
  return enabled ? `\u001b[${code}m${value}\u001b[0m` : value;
}

function statusColorCode(statusCode: number): number {
  if (statusCode >= 500) return 31;
  if (statusCode >= 400) return 33;
  return 32;
}

export function formatServerLogEvent(event: ServerLogEvent, options: FormatServerLogEventOptions = {}): string {
  const c = options.color;
  if (event.type === 'request') return `#${event.id} | ${color('request', 36, c)} [${color(event.method, 35, c)}] ${safeLogValue(event.path)}`;
  const statusColor = statusColorCode(event.statusCode);
  const details = [
    `#${event.id} | ${color('response', statusColor, c)} [${color(String(event.statusCode), statusColor, c)}] ${color(`${event.durationMs}ms`, 90, c)} [${color(event.method, 35, c)}] ${safeLogValue(event.path)}`,
  ];
  if (event.requestedModel) details.push(`requested=${safeLogValue(event.requestedModel)}`);
  if (event.modelId) details.push(`model=${safeLogValue(event.modelId)}`);
  if (event.routeReason) details.push(`route=${event.routeReason}`);
  if (event.group) details.push(`group=${event.group}`);
  if (typeof event.candidateCount === 'number') details.push(`candidates=${event.candidateCount}`);
  if (typeof event.triedCount === 'number') details.push(`tried=${event.triedCount}`);
  if (typeof event.inputTokens === 'number') details.push(`in=${event.inputTokens}`);
  if (typeof event.outputTokens === 'number') details.push(`out=${event.outputTokens}`);
  if (event.stream) details.push('stream=true');
  if (event.error) details.push(`error=${safeLogValue(event.error)}`);
  return details.join(' ');
}

function emitServerLog(logger: ServerOptions['requestLogger'], event: ServerLogEvent): void {
  try {
    logger?.(event);
  } catch {
    // Logging should never break proxying.
  }
}

function stringValue(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined;
}

async function readBody(req: IncomingMessage): Promise<any> {
  const chunks: Buffer[] = [];
  for await (const chunk of req) chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  const text = Buffer.concat(chunks).toString('utf8');
  if (!text) return {};
  try {
    return JSON.parse(text);
  } catch {
    throw Object.assign(new Error(`요청 본문을 파싱할 수 없어요. 유효한 JSON을 보내주세요. (${text.length}바이트 수신)`), { statusCode: 400 });
  }
}

function upstreamId(model: OmfmModel): string {
  return model.upstreamId ?? (sourceOf(model) === 'nvidia' ? model.id.replace(/^nvidia\//, '') : model.id);
}

function isCachedFreeModel(model: OmfmModel): boolean {
  if (sourceOf(model) === 'nvidia') return true;
  if (model.id.endsWith(':free')) return true;
  return Boolean(model.raw && typeof model.raw === 'object' && isFreeOpenRouterModel(model.raw as Parameters<typeof isFreeOpenRouterModel>[0]));
}

async function availableFreeModels(store: ConfigStore, apiKeys: ProviderApiKeys, fetchImpl?: FetchLike) {
  const catalog = await loadModelCatalog({ apiKeys, fetchImpl, store });
  return catalog.models.filter(isCachedFreeModel);
}

interface SelectedModelsResult {
  models: OmfmModel[];
  byId: Map<string, OmfmModel>;
  ids: string[];
  modelGroups: ModelGroups;
  defaultGroup?: string;
}

async function selectedModelSelection(store: ConfigStore, apiKeys: ProviderApiKeys, fetchImpl?: FetchLike): Promise<SelectedModelsResult> {
  const config = store.readConfig();
  const freeModels = await availableFreeModels(store, apiKeys, fetchImpl);
  const allIds = allGroupModelIds(config.modelGroups);
  const freeById = new Map(freeModels.map((model) => [model.id, model]));
  const cache = store.readModelCache();
  const cacheIds = new Set(cache?.models.map((m) => m.id) ?? []);
  const models: OmfmModel[] = [];
  const byId = new Map<string, OmfmModel>();
  for (const id of allIds) {
    const free = freeById.get(id);
    if (free) {
      models.push(free);
      byId.set(id, free);
    } else if (!cacheIds.has(id)) {
      const source: ModelSource = id.startsWith('nvidia/') ? 'nvidia' : 'openrouter';
      const stub: OmfmModel = { id, name: id, provider: source, source };
      models.push(stub);
      byId.set(id, stub);
    }
  }
  return {
    models,
    byId,
    ids: models.map((model) => model.id),
    modelGroups: config.modelGroups,
    defaultGroup: config.defaultGroup,
  };
}

function assertSelectedFree(models: OmfmModel[]): void {
  if (models.length === 0) {
    throw Object.assign(new Error('선택된 무료 모델이 없어요. config.json의 modelGroups에 사용할 모델을 하나 이상 추가하세요. (예: "nvidia/z-ai/glm-5.1")'), { statusCode: 400 });
  }
}

function missingKeyMessage(model: OmfmModel): string {
  const keyName = sourceOf(model) === 'nvidia' ? 'NVIDIA_API_KEY' : 'OPENROUTER_API_KEY';
  return `${keyName}가 없어서 ${model.id}을(를) 사용할 수 없어요. 환경변수 또는 .env 파일에 키를 추가하세요.`;
}

function withUpstreamModel(body: any, model: OmfmModel, stream?: boolean): any {
  const result = { ...body, model: upstreamId(model) };
  if (stream) result.stream_options = { include_usage: true };
  return result;
}

function requestedModelForRouting(models: OmfmModel[], requestedModel: unknown): string | undefined {
  if (typeof requestedModel !== 'string') return undefined;
  if (models.some((model) => model.id === requestedModel)) return requestedModel;
  const upstreamMatch = models.find((model) => upstreamId(model) === requestedModel);
  return upstreamMatch?.id ?? requestedModel;
}

function noUsableModelResponse(res: ServerResponse, lastError: unknown): void {
  json(res, 400, { error: { message: '설정된 API 키로 사용 가능한 무료 모델이 없어요. API 키 설정과 모델 ID를 확인하세요.', details: String(lastError ?? '') } });
}

function numberValue(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? Math.max(0, Math.floor(value)) : undefined;
}

function usageFromResponse(data: Record<string, any> | undefined): { inputTokens?: number; outputTokens?: number; totalTokens?: number } {
  const usage = data?.usage;
  if (!usage || typeof usage !== 'object') return {};
  const inputTokens = numberValue(usage.prompt_tokens) ?? numberValue(usage.input_tokens);
  const outputTokens = numberValue(usage.completion_tokens) ?? numberValue(usage.output_tokens);
  const totalTokens = numberValue(usage.total_tokens) ?? (inputTokens !== undefined || outputTokens !== undefined ? (inputTokens ?? 0) + (outputTokens ?? 0) : undefined);
  return { inputTokens, outputTokens, totalTokens };
}

function recordSuccessfulUsage(store: ConfigStore, model: OmfmModel, httpStatus: number, data?: Record<string, any>): void {
  const { inputTokens, outputTokens } = usageFromResponse(data);
  store.appendUsage({ ts: new Date().toISOString(), model: model.usageId ?? model.id, inputTokens: inputTokens ?? 0, outputTokens: outputTokens ?? 0, success: true });
}

async function recordUpstreamFailure(store: ConfigStore, model: OmfmModel, upstream: Response): Promise<string> {
  const text = await upstream.text();
  store.appendUsage({ ts: new Date().toISOString(), model: model.usageId ?? model.id, inputTokens: 0, outputTokens: 0, success: false });
  return `[${upstream.status}] ${text}`;
}

async function writeOpenAIAsAnthropic(upstream: Response, res: ServerResponse, body: any, modelId: string, onData?: (data?: Record<string, any>) => void): Promise<void> {
  if (body.stream) {
    onData?.();
    await pipeOpenAIStreamAsAnthropic(upstream.body, res, modelId);
    return;
  }
  const data = await upstream.json() as Record<string, any>;
  onData?.(data);
  json(res, upstream.status, openAIToAnthropic(data, modelId));
}

export function createOmfmServer(options: ServerOptions = {}): http.Server {
  const store = options.store ?? new ConfigStore();
  const env = options.env ?? process.env;
  const fetchImpl = options.fetchImpl;
  const requestLogger = options.requestLogger;
  let nextRequestId = 0;

  return http.createServer(async (req, res) => {
    const id = ++nextRequestId;
    const startedAt = Date.now();
    let requestedModel: string | undefined;
    let routedModel: string | undefined;
    let routeReason: RouteChoice['reason'] | 'failover' | undefined;
    let stream: boolean | undefined;
    let lastInputTokens: number | undefined;
    let lastOutputTokens: number | undefined;
    let lastError: string | undefined;
    let logGroup: string | undefined;
    let logCandidateCount: number | undefined;
    let logTriedCount: number | undefined;
    try {
      const method = req.method ?? 'GET';
      const url = new URL(req.url ?? '/', 'http://localhost');
      if (requestLogger) {
        emitServerLog(requestLogger, { type: 'request', id, method, path: url.pathname });
        res.once('finish', () => {
          emitServerLog(requestLogger, {
            type: 'response',
            id,
            method,
            path: url.pathname,
            statusCode: res.statusCode,
            durationMs: Date.now() - startedAt,
            requestedModel,
            modelId: routedModel,
            routeReason,
            stream,
            inputTokens: lastInputTokens,
            outputTokens: lastOutputTokens,
            error: lastError,
            group: logGroup,
            candidateCount: logCandidateCount,
            triedCount: logTriedCount,
          });
        });
      }
      if (method === 'GET' && url.pathname === '/health') {
        json(res, 200, { ok: true, service: 'sleepy-llm-router', version: VERSION, uptime: Math.floor(process.uptime()) });
        return;
      }

      if (method === 'GET' && url.pathname === '/v1/models') {
        const apiKeys = requireAnyProviderApiKey(env, store.paths.root);
        const selected = await selectedModelSelection(store, apiKeys, fetchImpl);
        json(res, 200, { object: 'list', data: selected.models.map((model) => ({ id: model.id, object: 'model', created: 0, owned_by: sourceOf(model), provider: model.provider })) });
        return;
      }

      if (method === 'POST' && (url.pathname === '/anthropic/v1/messages/count_tokens' || url.pathname === '/anthropic/messages/count_tokens')) {
        const body = await readBody(req);
        requestedModel = stringValue(body.model);
        json(res, 200, { input_tokens: estimateInputTokens(body) });
        return;
      }

      if (method === 'POST' && url.pathname === '/v1/chat/completions') {
        const apiKeys = requireAnyProviderApiKey(env, store.paths.root);
        const body = await readBody(req);
        requestedModel = stringValue(body.model);
        stream = Boolean(body.stream);
        const selected = await selectedModelSelection(store, apiKeys, fetchImpl);
        assertSelectedFree(selected.models);
        const routingModel = requestedModelForRouting(selected.models, body.model);
        const candidateIds = orderedCandidates(selected.modelGroups, routingModel, selected.defaultGroup);
        const normalized = normalizeModelGroupName(routingModel);
        logGroup = normalized && selected.modelGroups[normalized] ? normalized : selected.defaultGroup;
        logCandidateCount = candidateIds.length;
        let upstreamError: unknown;
        let triedAny = false;
        let triedCount = 0;
        for (const modelId of candidateIds) {
          const model = selected.byId.get(modelId);
          if (!model) continue;
          const apiKey = apiKeys[sourceOf(model)];
          if (!apiKey) {
            upstreamError = missingKeyMessage(model);
            lastError = String(upstreamError);
            continue;
          }
          if (requestLogger) {
            routedModel = modelId;
            routeReason = 'fallback-order';
          }
          triedAny = true;
          triedCount += 1;
          const upstreamBody = withUpstreamModel(body, model, stream);
          const upstream = sourceOf(model) === 'nvidia'
            ? await postNvidiaChatCompletion({ apiKey, body: upstreamBody, fetchImpl })
            : await postOpenRouterChatCompletion({ apiKey, body: upstreamBody, stream, fetchImpl });
          if (upstream.ok) {
            if (stream) {
              res.writeHead(upstream.status, { 'Content-Type': upstream.headers.get('content-type') ?? 'text/event-stream; charset=utf-8' });
              const streamUsage = await pipeWebStreamToNode(upstream.body, res);
              lastInputTokens = streamUsage.inputTokens;
              lastOutputTokens = streamUsage.outputTokens;
              store.appendUsage({ ts: new Date().toISOString(), model: model.usageId ?? model.id, inputTokens: streamUsage.inputTokens ?? 0, outputTokens: streamUsage.outputTokens ?? 0, success: true });
              logTriedCount = triedCount;
              return;
            }
            const data = await upstream.json() as Record<string, any>;
            // ponytail: 빈 choices는 실패로 간주하고 다음 모델 시도
            if (!Array.isArray(data.choices) || data.choices.length === 0) {
              upstreamError = '.choices가 비어있어요';
              lastError = `[${modelId}] choices가 비어있어요`;
              recordSuccessfulUsage(store, model, upstream.status);
              continue;
            }
            const usage = usageFromResponse(data);
            lastInputTokens = usage.inputTokens;
            lastOutputTokens = usage.outputTokens;
            recordSuccessfulUsage(store, model, upstream.status, data);
            logTriedCount = triedCount;
            json(res, upstream.status, data);
            return;
          }
          upstreamError = await recordUpstreamFailure(store, model, upstream);
          lastError = `[${modelId}] ${String(upstreamError).slice(0, 300)}`;
        }
        if (!triedAny) {
          noUsableModelResponse(res, upstreamError);
          return;
        }
        json(res, 502, { error: { message: '선택된 모든 무료 모델이 실패했어요.', details: String(upstreamError ?? '') } });
        return;
      }

      if (method === 'POST' && (url.pathname === '/anthropic/v1/messages' || url.pathname === '/anthropic/messages')) {
        const apiKeys = requireAnyProviderApiKey(env, store.paths.root);
        const body = await readBody(req);
        requestedModel = stringValue(body.model);
        stream = Boolean(body.stream);
        const selected = await selectedModelSelection(store, apiKeys, fetchImpl);
        assertSelectedFree(selected.models);
        const routingModel = requestedModelForRouting(selected.models, body.model);
        const candidateIds = orderedCandidates(selected.modelGroups, routingModel, selected.defaultGroup);
        const normalized = normalizeModelGroupName(routingModel);
        logGroup = normalized && selected.modelGroups[normalized] ? normalized : selected.defaultGroup;
        logCandidateCount = candidateIds.length;
        let upstreamError: unknown;
        let triedAny = false;
        let triedCount = 0;
        for (const modelId of candidateIds) {
          const model = selected.byId.get(modelId);
          if (!model) continue;
          const apiKey = apiKeys[sourceOf(model)];
          if (!apiKey) {
            upstreamError = missingKeyMessage(model);
            lastError = String(upstreamError);
            continue;
          }
          if (requestLogger) {
            routedModel = modelId;
            routeReason = 'fallback-order';
          }
          triedAny = true;
          triedCount += 1;
          if (sourceOf(model) === 'nvidia') {
            const fallbackBody = anthropicToOpenAI(body, upstreamId(model));
            const upstream = await postNvidiaChatCompletion({ apiKey, body: fallbackBody, fetchImpl });
            if (upstream.ok) {
              logTriedCount = triedCount;
              await writeOpenAIAsAnthropic(upstream, res, body, modelId, (data) => {
                const usage = usageFromResponse(data);
                lastInputTokens = usage.inputTokens;
                lastOutputTokens = usage.outputTokens;
                recordSuccessfulUsage(store, model, upstream.status, data);
              });
              return;
            }
            upstreamError = await recordUpstreamFailure(store, model, upstream);
            lastError = `[${modelId}] ${String(upstreamError).slice(0, 300)}`;
            continue;
          }

          const upstreamBody = withUpstreamModel(body, model, stream);
          let upstream = await postOpenRouterAnthropicMessage({ apiKey, body: upstreamBody, fetchImpl });
          if (!upstream.ok && (upstream.status === 404 || upstream.status === 405)) {
            const fallbackBody = anthropicToOpenAI(body, upstreamId(model));
            if (stream) fallbackBody.stream_options = { include_usage: true };
            upstream = await postOpenRouterChatCompletion({ apiKey, body: fallbackBody, stream, fetchImpl });
            if (upstream.ok) {
              logTriedCount = triedCount;
              await writeOpenAIAsAnthropic(upstream, res, body, modelId, (data) => {
                const usage = usageFromResponse(data);
                lastInputTokens = usage.inputTokens;
                lastOutputTokens = usage.outputTokens;
                recordSuccessfulUsage(store, model, upstream.status, data);
              });
              return;
            }
          }
          if (upstream.ok) {
            if (stream) {
              res.writeHead(upstream.status, { 'Content-Type': upstream.headers.get('content-type') ?? 'text/event-stream; charset=utf-8' });
              const streamUsage = await pipeWebStreamToNode(upstream.body, res);
              lastInputTokens = streamUsage.inputTokens;
              lastOutputTokens = streamUsage.outputTokens;
              store.appendUsage({ ts: new Date().toISOString(), model: model.usageId ?? model.id, inputTokens: streamUsage.inputTokens ?? 0, outputTokens: streamUsage.outputTokens ?? 0, success: true });
              logTriedCount = triedCount;
              return;
            }
            const data = await upstream.json() as Record<string, any>;
            // ponytail: 빈 choices/content는 실패로 간주
            const empty = !Array.isArray(data.choices) && !Array.isArray(data.content);
            if (empty) {
              upstreamError = 'choices와 content가 모두 비어있어요';
              lastError = `[${modelId}] choices와 content가 모두 비어있어요`;
              recordSuccessfulUsage(store, model, upstream.status);
              continue;
            }
            const usage = usageFromResponse(data);
            lastInputTokens = usage.inputTokens;
            lastOutputTokens = usage.outputTokens;
            recordSuccessfulUsage(store, model, upstream.status, data);
            logTriedCount = triedCount;
            json(res, upstream.status, data);
            return;
          }
          upstreamError = await recordUpstreamFailure(store, model, upstream);
          lastError = `[${modelId}] ${String(upstreamError).slice(0, 300)}`;
        }
        if (!triedAny) {
          noUsableModelResponse(res, upstreamError);
          return;
        }
        json(res, 502, { error: { type: 'api_error', message: '선택된 모든 무료 모델이 실패했어요.', details: String(upstreamError ?? '') } });
        return;
      }

      json(res, 404, { error: { message: `지원하지 않는 엔드포인트예요: ${method} ${url.pathname}. 사용 가능한 엔드포인트: GET /health, GET /v1/models, POST /v1/chat/completions, POST /anthropic/v1/messages` } });
    } catch (error) {
      const statusCode = typeof (error as { statusCode?: unknown }).statusCode === 'number' ? (error as { statusCode: number }).statusCode : 500;
      const method = req.method ?? 'GET';
      const path = req.url ?? '/';
      const msg = error instanceof Error ? error.message : String(error);
      json(res, statusCode, { error: { message: `${msg}`, request: `${method} ${path}` } });
    }
  });
}

export async function listen(server: http.Server, port: number): Promise<number> {
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject);
    server.listen(port, '127.0.0.1', () => resolve());
  });
  const address = server.address();
  return typeof address === 'object' && address ? address.port : port;
}

function estimateInputTokens(body: unknown): number {
  const text = JSON.stringify(body ?? {});
  return Math.max(1, Math.ceil(text.length / 4));
}
