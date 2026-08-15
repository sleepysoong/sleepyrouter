// HTTP Server - mirrors internal/srv/
import { createRouter, type HandlerDeps, type ServerLogEvent } from "./handler.js";
import { ConfigStore } from "./config.js";

export interface ServerOptions {
  store?: ConfigStore;
  env?: Record<string, string | undefined>;
  requestLogger?: (event: ServerLogEvent) => void;
  startTime?: Date;
}

export function createSleepyRouterServer(options: ServerOptions) {
  const store = options.store ?? new ConfigStore();
  const env = options.env ?? process.env;
  const requestLogger = options.requestLogger;
  const startTime = options.startTime ?? new Date();

  const deps: HandlerDeps = { store, env, requestLogger };
  const router = createRouter(deps);
  let nextId = 0;

  async function handleRequest(req: Request): Promise<Response> {
    const id = ++nextId;
    const startedAt = Date.now();
    const url = new URL(req.url);
    const method = req.method;
    const path = url.pathname;

    if (requestLogger) {
      requestLogger({
        type: "request",
        id,
        method,
        path,
      });
    }

    let response: Response;
    try {
      response = await routeRequest(router, method, path, req, id);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      response = Response.json(
        { error: { message: msg } },
        { status: 500 },
      );
    }

    if (requestLogger) {
      requestLogger({
        type: "response",
        id,
        method,
        path,
        statusCode: response.status,
        durationMs: Date.now() - startedAt,
      });
    }

    return response;
  }

  return { fetch: handleRequest, store, startTime };
}

async function routeRequest(
  router: ReturnType<typeof createRouter>,
  method: string,
  path: string,
  req: Request,
  requestId: number,
): Promise<Response> {
  // GET /health
  if (method === "GET" && path === "/health") {
    return router.handleHealth();
  }

  // GET /v1/models
  if (method === "GET" && path === "/v1/models") {
    return router.handleModels();
  }

  // POST /v1/chat/completions
  if (method === "POST" && path === "/v1/chat/completions") {
    const body = await req.json();
    return router.handleChat(body, "openai", requestId);
  }

  // POST /anthropic/v1/messages or /anthropic/messages
  if (
    method === "POST" &&
    (path === "/anthropic/v1/messages" || path === "/anthropic/messages")
  ) {
    const body = await req.json();
    return router.handleChat(body, "anthropic", requestId);
  }

  // POST /anthropic/v1/messages/count_tokens or /anthropic/messages/count_tokens
  if (
    method === "POST" &&
    (path === "/anthropic/v1/messages/count_tokens" ||
      path === "/anthropic/messages/count_tokens")
  ) {
    const body = await req.json();
    return router.handleCountTokens(body);
  }

  // 404
  return Response.json(
    {
      error: {
        message: `${method} ${path}은(는) 지원하지 않는 경로예요.`,
        available: [
          "GET /health",
          "GET /v1/models",
          "POST /v1/chat/completions",
          "POST /anthropic/v1/messages",
          "POST /anthropic/messages",
        ],
      },
    },
    { status: 404 },
  );
}

export function listenServer(
  server: ReturnType<typeof createSleepyRouterServer>,
  port: number,
): ReturnType<typeof Bun.serve> {
  return Bun.serve({
    port,
    hostname: "127.0.0.1",
    fetch: server.fetch,
  });
}
