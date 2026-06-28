import http, { IncomingMessage, ServerResponse } from 'node:http';
import { ConfigStore } from '../config/store.js';
import { requireAnyProviderApiKey } from '../config/env.js';
import { loadModelCatalog } from '../providers/catalog.js';
import { postNvidiaChatCompletion } from '../providers/nvidia.js';
import { isFreeOpenRouterModel, postOpenRouterAnthropicMessage, postOpenRouterChatCompletion } from '../providers/openrouter.js';
import { FetchLike, ModelGroups, ModelSource, OmfmModel, ProviderApiKeys, sourceOf } from '../types.js';
import { orderedCandidates, RouteChoice } from '../latency/router.js';
import { allGroupModelIds } from '../model-groups.js';
import { anthropicToOpenAI, openAIToAnthropic } from './translate.js';
import { pipeOpenAIStreamAsAnthropic, pipeWebStreamToNode } from './sse.js';

export interface ServerOptions {
  store?: ConfigStore;
  fetchImpl?: FetchLike;
  env?: NodeJS.ProcessEnv;
  maxRetries?: number;
  requestLogger?: (event: ServerLogEvent) => void;
}

function json(res: ServerResponse, status: number, body: unknown): void {
  res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8' });
  res.end(JSON.stringify(body));
}

export type ServerLogEvent =
  | { type: 'request'; id: number; method: string; path: string }
  | { type: 'response'; id: number; method: string; path: string; statusCode: number; durationMs: number; requestedModel?: string; modelId?: string; routeReason?: RouteChoice['reason'] | 'failover'; stream?: boolean; inputTokens?: number; outputTokens?: number; error?: string };

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
  if (event.type === 'request') return `[slr] #${event.id} ${color('request', 36, options.color)} ${event.method} ${safeLogValue(event.path)}`;
  const statusColor = statusColorCode(event.statusCode);
  const details = [
    `[slr] #${event.id} ${color('response', statusColor, options.color)}`,
    color(String(event.statusCode), statusColor, options.color),
    `${event.durationMs}ms`,
    event.method,
    safeLogValue(event.path),
  ];
  if (event.requestedModel) details.push(`requested=${safeLogValue(event.requestedModel)}`);
  if (event.modelId) details.push(`model=${safeLogValue(event.modelId)}`);
  if (event.routeReason) details.push(`route=${event.routeReason}`);
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
  return JSON.parse(text);
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
    throw Object.assign(new Error('선택된 무료 모델이 없어요. `slr model`에서 사용할 무료 모델을 하나 이상 선택하세요.'), { statusCode: 400 });
  }
}

function missingKeyMessage(model: OmfmModel): string {
  return `${sourceOf(model) === 'nvidia' ? 'NVIDIA_API_KEY' : 'OPENROUTER_API_KEY'} is required for ${model.id}.`;
}

function withUpstreamModel(body: any, model: OmfmModel): any {
  return { ...body, model: upstreamId(model) };
}

function requestedModelForRouting(models: OmfmModel[], requestedModel: unknown): string | undefined {
  if (typeof requestedModel !== 'string') return undefined;
  if (models.some((model) => model.id === requestedModel)) return requestedModel;
  const upstreamMatch = models.find((model) => upstreamId(model) === requestedModel);
  return upstreamMatch?.id ?? requestedModel;
}

function noUsableModelResponse(res: ServerResponse, lastError: unknown): void {
  json(res, 400, { error: { message: '설정된 프로바이더 API 키로 사용 가능한 선택된 무료 모델이 없어요.', details: String(lastError ?? '') } });
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
  store.recordUsage(model.usageId ?? model.id, { success: true, httpStatus, ...usageFromResponse(data) });
}

async function recordUpstreamFailure(store: ConfigStore, model: OmfmModel, upstream: Response): Promise<string> {
  const text = await upstream.text();
  const status = upstream.status === 429 ? 'rate-limited' : upstream.status === 402 ? 'payment' : 'failed';
  store.recordUsage(model.usageId ?? model.id, { success: false, httpStatus: upstream.status, status });
  return text;
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
  const maxRetries = options.maxRetries ?? 2;
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
          });
        });
      }
      if (method === 'GET' && url.pathname === '/health') {
        json(res, 200, { ok: true, service: 'sleepy-llm-router' });
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
        let upstreamError: unknown;
        let attempts = 0;
        for (const modelId of candidateIds) {
          if (attempts >= maxRetries) break;
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
            routeReason = attempts === 0 ? 'fallback-order' : 'failover';
          }
          attempts += 1;
          const upstreamBody = withUpstreamModel(body, model);
          const upstream = sourceOf(model) === 'nvidia'
            ? await postNvidiaChatCompletion({ apiKey, body: upstreamBody, fetchImpl })
            : await postOpenRouterChatCompletion({ apiKey, body: upstreamBody, stream, fetchImpl });
          if (upstream.ok) {
            if (stream) {
              recordSuccessfulUsage(store, model, upstream.status);
              res.writeHead(upstream.status, { 'Content-Type': upstream.headers.get('content-type') ?? 'text/event-stream; charset=utf-8' });
              await pipeWebStreamToNode(upstream.body, res);
              return;
            }
            const data = await upstream.json() as Record<string, any>;
            const usage = usageFromResponse(data);
            lastInputTokens = usage.inputTokens;
            lastOutputTokens = usage.outputTokens;
            recordSuccessfulUsage(store, model, upstream.status, data);
            json(res, upstream.status, data);
            return;
          }
          upstreamError = await recordUpstreamFailure(store, model, upstream);
          lastError = `[${modelId}] ${String(upstreamError).slice(0, 300)}`;
        }
        if (attempts === 0) {
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
        let upstreamError: unknown;
        let attempts = 0;
        for (const modelId of candidateIds) {
          if (attempts >= maxRetries) break;
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
            routeReason = attempts === 0 ? 'fallback-order' : 'failover';
          }
          attempts += 1;
          if (sourceOf(model) === 'nvidia') {
            const fallbackBody = anthropicToOpenAI(body, upstreamId(model));
            const upstream = await postNvidiaChatCompletion({ apiKey, body: fallbackBody, fetchImpl });
            if (upstream.ok) {
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

          const upstreamBody = withUpstreamModel(body, model);
          let upstream = await postOpenRouterAnthropicMessage({ apiKey, body: upstreamBody, fetchImpl });
          if (!upstream.ok && (upstream.status === 404 || upstream.status === 405)) {
            const fallbackBody = anthropicToOpenAI(body, upstreamId(model));
            upstream = await postOpenRouterChatCompletion({ apiKey, body: fallbackBody, stream, fetchImpl });
            if (upstream.ok) {
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
              recordSuccessfulUsage(store, model, upstream.status);
              res.writeHead(upstream.status, { 'Content-Type': upstream.headers.get('content-type') ?? 'text/event-stream; charset=utf-8' });
              await pipeWebStreamToNode(upstream.body, res);
              return;
            }
            const data = await upstream.json() as Record<string, any>;
            const usage = usageFromResponse(data);
            lastInputTokens = usage.inputTokens;
            lastOutputTokens = usage.outputTokens;
            recordSuccessfulUsage(store, model, upstream.status, data);
            json(res, upstream.status, data);
            return;
          }
          upstreamError = await recordUpstreamFailure(store, model, upstream);
          lastError = `[${modelId}] ${String(upstreamError).slice(0, 300)}`;
        }
        if (attempts === 0) {
          noUsableModelResponse(res, upstreamError);
          return;
        }
        json(res, 502, { error: { type: 'api_error', message: '선택된 모든 무료 모델이 실패했어요.', details: String(upstreamError ?? '') } });
        return;
      }

      json(res, 404, { error: { message: `Unsupported endpoint: ${method} ${url.pathname}` } });
    } catch (error) {
      const statusCode = typeof (error as { statusCode?: unknown }).statusCode === 'number' ? (error as { statusCode: number }).statusCode : 500;
      json(res, statusCode, { error: { message: error instanceof Error ? error.message : String(error) } });
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
