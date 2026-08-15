"""Candidate failover and retry processing via LiteLLM."""

import datetime
from typing import Any

from fastapi.responses import JSONResponse, StreamingResponse
from litellm import acompletion

from sleepyrouter.config import ConfigStore, api_key_for
from sleepyrouter.protocol import (
    default_protocol_transformer_registry,
)
from sleepyrouter.providers import map_to_litellm_kwargs
from sleepyrouter.types import ProviderAPIKeys, SleepyRouterModel, UsageLogEntry
from sleepyrouter.utils import truncate

from .stream import create_sse_stream_generator


async def process_chat_candidates(
    store: ConfigStore,
    api_keys: ProviderAPIKeys,
    by_id: dict[str, SleepyRouterModel],
    candidates: list[str],
    body: dict[str, Any],
    is_stream: bool,
    api_type: str,
) -> Any:
    upstream_error = ""
    tried_any = False

    transformer = default_protocol_transformer_registry.get(api_type)

    for model_id in candidates:
        model = by_id.get(model_id)
        if not model:
            continue

        tried_any = True
        upstream_model_id = model.upstream_id or model.id
        api_key = api_key_for(api_keys, model.source)

        if not api_key:
            upstream_error = f"[{model_id}] API key missing for provider {model.source}"
            continue

        request_kwargs = transformer.transform_request(
            body, upstream_model_id, model.provider
        )

        try:
            litellm_kwargs = map_to_litellm_kwargs(model, api_key, request_kwargs)
            litellm_kwargs["num_retries"] = 0

            if is_stream:
                response_gen = await acompletion(**litellm_kwargs, stream=True)
                media_type = (
                    "text/event-stream" if api_type == "anthropic" else "text/plain"
                )
                generator = create_sse_stream_generator(
                    response_gen, api_type, model, store
                )
                return StreamingResponse(generator, media_type=media_type)

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

            transformed_resp = transformer.transform_response(
                resp_dict, upstream_model_id
            )
            return JSONResponse(content=transformed_resp)

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
