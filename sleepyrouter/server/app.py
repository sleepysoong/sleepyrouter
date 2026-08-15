"""FastAPI application initialization and route definitions."""

import time
from typing import Any

from fastapi import FastAPI, Request, Response
from fastapi.responses import JSONResponse

from sleepyrouter.config import ConfigStore, require_any_provider_api_key
from sleepyrouter.protocol import estimate_input_tokens
from sleepyrouter.routing import all_group_model_ids, default_routing_engine
from sleepyrouter.types import SleepyRouterConfig, SleepyRouterModel, source_of

from .failover import process_chat_candidates

VERSION = "0.0.4"


def create_app(
    store: ConfigStore | None = None, env: dict[str, str] | None = None
) -> FastAPI:
    app = FastAPI(title="sleepyrouter", version=VERSION)
    store = store or ConfigStore()
    start_time = time.time()

    def get_selected_models(
        api_keys: Any,
    ) -> tuple[
        list[SleepyRouterModel], dict[str, SleepyRouterModel], SleepyRouterConfig
    ]:
        config = store.read_config()
        all_ids = all_group_model_ids(config.model_groups, *config.group_order)
        models: list[SleepyRouterModel] = []
        by_id: dict[str, SleepyRouterModel] = {}
        for mid in all_ids:
            def_obj = (config.models or {}).get(mid)
            if not def_obj:
                continue
            m = SleepyRouterModel(
                id=mid,
                upstream_id=def_obj.name,
                provider=def_obj.provider,
                source=def_obj.provider,
                usage_id=mid,
                api_base=def_obj.api_base,
            )
            models.append(m)
            by_id[mid] = m
        return models, by_id, config

    @app.get("/health")
    async def health() -> dict[str, Any]:
        return {
            "ok": True,
            "service": "sleepyrouter",
            "version": VERSION,
            "uptime": int(time.time() - start_time),
        }

    @app.get("/v1/models")
    async def models() -> dict[str, Any]:
        api_keys = require_any_provider_api_key(env, store.root)
        models_list, _, _ = get_selected_models(api_keys)
        data = [
            {
                "id": m.id,
                "object": "model",
                "created": 0,
                "owned_by": source_of(m),
                "provider": m.provider,
            }
            for m in models_list
        ]
        return {"object": "list", "data": data}

    @app.post("/anthropic/v1/messages/count_tokens")
    @app.post("/anthropic/messages/count_tokens")
    async def count_tokens(request: Request) -> dict[str, Any]:
        body = await request.json()
        return {"input_tokens": estimate_input_tokens(body)}

    async def handle_chat_completion(body: dict[str, Any], api_type: str) -> Response:
        api_keys = require_any_provider_api_key(env, store.root)
        models_list, by_id, config = get_selected_models(api_keys)

        if not models_list:
            return JSONResponse(
                status_code=400,
                content={
                    "error": {
                        "message": "선택된 무료 모델이 없어요. config.json의 modelGroups에 사용할 모델을 하나 이상 추가하세요."
                    }
                },
            )

        requested_model = str(body.get("model", ""))
        is_stream = bool(body.get("stream"))

        candidates, _candidate_reason = default_routing_engine.resolve(
            config.model_groups,
            requested_model,
            config.default_model_group,
            config.group_order,
        )

        return await process_chat_candidates(
            store, api_keys, by_id, candidates, body, is_stream, api_type
        )

    @app.post("/v1/chat/completions")
    async def chat_completions(request: Request) -> Response:
        body = await request.json()
        return await handle_chat_completion(body, "openai")

    @app.post("/anthropic/v1/messages")
    @app.post("/anthropic/messages")
    async def anthropic_messages(request: Request) -> Response:
        body = await request.json()
        return await handle_chat_completion(body, "anthropic")

    return app
