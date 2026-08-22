"""FastAPI application initialization and route definitions."""

import itertools
import time
from typing import Any

from fastapi import FastAPI, Request, Response
from fastapi.responses import JSONResponse
import litellm

from sleepyrouter.config import ConfigStore
from sleepyrouter.events import (
    CandidatesResolvedEvent,
    RequestReceivedEvent,
    default_event_bus,
)
from sleepyrouter.providers import require_any_provider_api_key
from sleepyrouter.routing import all_group_model_ids, ordered_candidates
from sleepyrouter.types import SleepyRouterConfig, SleepyRouterModel, source_of

from .failover import process_chat_candidates

# Configure LiteLLM globally
litellm.drop_params = True
litellm.suppress_debug_info = True

VERSION = "1.0.0"
_request_counter = itertools.count(1)


def _build_selected_models(
    store: ConfigStore,
) -> tuple[list[SleepyRouterModel], dict[str, SleepyRouterModel], SleepyRouterConfig]:
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


def create_app(store: ConfigStore | None = None, env: dict[str, str] | None = None) -> FastAPI:
    app = FastAPI(title="sleepyrouter", version=VERSION)
    active_store = store or ConfigStore()
    start_time = time.time()

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
        require_any_provider_api_key(env, active_store.root)
        models_list, _, _ = _build_selected_models(active_store)
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

    @app.post("/v1/chat/completions")
    async def chat_completions(request: Request) -> Response:
        body = await request.json()
        req_id = next(_request_counter)
        requested_model = str(body.get("model", ""))
        is_stream = bool(body.get("stream"))

        default_event_bus.publish(
            RequestReceivedEvent(
                ts=time.time(),
                request_id=req_id,
                method="POST",
                path="/v1/chat/completions",
                requested_model=requested_model,
                is_stream=is_stream,
            )
        )

        require_any_provider_api_key(env, active_store.root)
        models_list, by_id, config = _build_selected_models(active_store)

        if not models_list:
            err_msg = (
                "선택된 무료 모델이 없어요. config.json의 modelGroups에 "
                "사용할 모델을 하나 이상 추가하세요."
            )
            return JSONResponse(status_code=400, content={"error": {"message": err_msg}})

        candidates, candidate_reason = ordered_candidates(
            config.model_groups,
            requested_model,
            config.default_model_group,
            *config.group_order,
            known_models=by_id,
        )

        default_event_bus.publish(
            CandidatesResolvedEvent(
                ts=time.time(),
                request_id=req_id,
                requested_model=requested_model,
                candidates=candidates,
                route_reason=candidate_reason,
            )
        )

        return await process_chat_candidates(
            active_store,
            by_id,
            candidates,
            body,
            request_id=req_id,
            is_stream=is_stream,
            env=env,
        )

    return app
