"""FastAPI server for sleepyrouter using LiteLLM."""

import datetime
import json
import os
import time
from typing import Any

from fastapi import FastAPI, Request, Response
from fastapi.responses import JSONResponse, StreamingResponse
from litellm import acompletion

from .config import ConfigStore, require_any_provider_api_key
from .protocol import (
    anthropic_to_openai,
    estimate_input_tokens,
    openai_to_anthropic,
)
from .providers import map_to_litellm_kwargs
from .routing import all_group_model_ids, ordered_candidates
from .types import SleepyRouterConfig, SleepyRouterModel, UsageLogEntry, source_of
from .utils import truncate

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
            )
            models.append(m)
            by_id[mid] = m
        return models, by_id, config

    @app.get("/health")
    async def health():
        return {
            "ok": True,
            "service": "sleepyrouter",
            "version": VERSION,
            "uptime": int(time.time() - start_time),
        }

    @app.get("/v1/models")
    async def models():
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
    async def count_tokens(request: Request):
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

        candidates, _candidate_reason = ordered_candidates(
            config.model_groups,
            requested_model,
            config.default_model_group,
            *config.group_order,
        )

        upstream_error = ""
        tried_any = False

        for model_id in candidates:
            model = by_id.get(model_id)
            if not model:
                continue

            tried_any = True
            upstream_model_id = model.upstream_id or model.id

            source = source_of(model)
            if source == "openrouter":
                api_key = api_keys.open_router
            elif source == "nvidia":
                api_key = api_keys.nvidia
            elif source == "copilot":
                api_key = api_keys.copilot
            elif source == "zen":
                api_key = api_keys.zen
            elif source == "google":
                api_key = api_keys.google
            else:
                api_key = os.environ.get(f"{source.upper()}_API_KEY", "")

            if not api_key:
                upstream_error = f"[{model_id}] API key missing for provider {source}"
                continue

            # Request translation
            if api_type == "anthropic":
                request_kwargs = anthropic_to_openai(body, upstream_model_id)
            else:
                request_kwargs = dict(body)
                request_kwargs["model"] = upstream_model_id

            try:
                litellm_kwargs = map_to_litellm_kwargs(model, api_key, request_kwargs)
                # Ensure num_retries is 0 so sleepyrouter handles candidate failover
                litellm_kwargs["num_retries"] = 0

                if is_stream:
                    response_gen = await acompletion(**litellm_kwargs, stream=True)

                    async def stream_generator(current_gen=response_gen, target_model=model):
                        input_tokens = 0
                        output_tokens = 0
                        try:
                            async for chunk in current_gen:
                                if hasattr(chunk, "usage") and chunk.usage:
                                    input_tokens = (
                                        getattr(chunk.usage, "prompt_tokens", 0) or 0
                                    )
                                    output_tokens = (
                                        getattr(chunk.usage, "completion_tokens", 0)
                                        or 0
                                    )

                                if api_type == "anthropic":
                                    delta_text = ""
                                    if hasattr(chunk, "choices") and chunk.choices:
                                        delta_text = (
                                            getattr(
                                                chunk.choices[0].delta, "content", ""
                                            )
                                            or ""
                                        )
                                    if delta_text:
                                        frame = f"event: content_block_delta\ndata: {json.dumps({'type': 'content_block_delta', 'index': 0, 'delta': {'type': 'text_delta', 'text': delta_text}})}\n\n"
                                        yield frame.encode()
                                else:
                                    chunk_dict = (
                                        chunk.model_dump()
                                        if hasattr(chunk, "model_dump")
                                        else dict(chunk)
                                    )
                                    yield f"data: {json.dumps(chunk_dict)}\n\n".encode()

                            if api_type == "anthropic":
                                yield b'event: message_delta\ndata: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":0}}\n\n'
                                yield b'event: message_stop\ndata: {"type":"message_stop"}\n\n'
                            else:
                                yield b"data: [DONE]\n\n"

                            store.append_usage(
                                UsageLogEntry(
                                    ts=datetime.datetime.now(
                                        datetime.timezone.utc
                                    ).isoformat(),
                                    model=target_model.usage_id or target_model.id,
                                    input_tokens=input_tokens,
                                    output_tokens=output_tokens,
                                    success=True,
                                )
                            )
                        except (RuntimeError, OSError, ValueError):
                            store.append_usage(
                                UsageLogEntry(
                                    ts=datetime.datetime.now(
                                        datetime.timezone.utc
                                    ).isoformat(),
                                    model=target_model.usage_id or target_model.id,
                                    input_tokens=0,
                                    output_tokens=0,
                                    success=False,
                                )
                            )

                    media_type = (
                        "text/event-stream" if api_type == "anthropic" else "text/plain"
                    )
                    return StreamingResponse(stream_generator(), media_type=media_type)

                # Non-streaming response
                response_obj = await acompletion(**litellm_kwargs)
                resp_dict = (
                    response_obj.model_dump()
                    if hasattr(response_obj, "model_dump")
                    else dict(response_obj)
                )

                usage_data = resp_dict.get("usage") or {}
                in_tok = usage_data.get("prompt_tokens") or 0
                out_tok = usage_data.get("completion_tokens") or 0

                store.append_usage(
                    UsageLogEntry(
                        ts=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                        model=model.usage_id or model.id,
                        input_tokens=in_tok,
                        output_tokens=out_tok,
                        success=True,
                    )
                )

                if api_type == "anthropic":
                    anthropic_resp = openai_to_anthropic(resp_dict, upstream_model_id)
                    return JSONResponse(content=anthropic_resp)

                return JSONResponse(content=resp_dict)

            except (RuntimeError, ValueError, KeyError, OSError) as e:
                err_msg = str(e)
                upstream_error = f"[{model_id}] {truncate(err_msg, 300)}"
                store.append_usage(
                    UsageLogEntry(
                        ts=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                        model=model.usage_id or model.id,
                        input_tokens=0,
                        output_tokens=0,
                        success=False,
                    )
                )
                continue

        if not tried_any:
            return JSONResponse(
                status_code=502,
                content={
                    "error": {
                        "message": "사용 가능한 모델이 없어요. API 키를 확인하세요.",
                        "details": upstream_error,
                    }
                },
            )

        extras = {"details": upstream_error}
        if api_type == "anthropic":
            extras["type"] = "api_error"

        return JSONResponse(
            status_code=502,
            content={
                "error": {
                    "message": "선택된 모든 무료 모델이 실패했어요.",
                    **extras,
                }
            },
        )

    @app.post("/v1/chat/completions")
    async def chat_completions(request: Request):
        body = await request.json()
        return await handle_chat_completion(body, "openai")

    @app.post("/anthropic/v1/messages")
    @app.post("/anthropic/messages")
    async def anthropic_messages(request: Request):
        body = await request.json()
        return await handle_chat_completion(body, "anthropic")

    return app
