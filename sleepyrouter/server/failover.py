"""Candidate failover and retry processing via LiteLLM."""

import datetime
import os
import time
from typing import Any

from fastapi import Response
from fastapi.responses import JSONResponse, StreamingResponse
from litellm import acompletion

from sleepyrouter.config import ConfigStore, api_key_for
from sleepyrouter.events import (
    AllCandidatesFailedEvent,
    CandidateAttemptEvent,
    CandidateFailedEvent,
    CandidateSucceededEvent,
    FailoverEvent,
    default_event_bus,
)
from sleepyrouter.protocol import (
    default_protocol_transformer_registry,
)
from sleepyrouter.providers import map_to_litellm_kwargs
from sleepyrouter.providers.antigravity import (
    call_antigravity_completion,
    call_antigravity_stream,
)
from sleepyrouter.types import ProviderAPIKeys, SleepyRouterModel, UsageLogEntry
from sleepyrouter.utils import truncate

from .stream import create_sse_stream_generator


async def _execute_candidate_attempt(
    model: SleepyRouterModel,
    api_key: str,
    body: dict[str, Any],
    api_type: str,
    store: ConfigStore,
    *,
    request_id: int,
    index: int,
    total: int,
    is_stream: bool,
    default_timeout: float,
) -> Response:
    attempt_start = time.time()
    upstream_model_id = model.upstream_id or model.id
    transformer = default_protocol_transformer_registry.get(api_type)
    request_kwargs = transformer.transform_request(body, upstream_model_id, model.provider)

    default_event_bus.publish(
        CandidateAttemptEvent(
            ts=time.time(),
            request_id=request_id,
            index=index,
            total=total,
            model_id=model.id,
            provider=model.provider,
            upstream_id=upstream_model_id,
        )
    )

    # Direct Antigravity gateway routing when no custom api_base is configured
    if model.source == "antigravity" and not model.api_base:
        if is_stream:
            antigravity_gen = call_antigravity_stream(
                upstream_model_id,
                api_key,
                request_kwargs,
                timeout=default_timeout,
            )
            media_type = "text/event-stream" if api_type == "anthropic" else "text/plain"
            generator = create_sse_stream_generator(
                antigravity_gen,
                api_type,
                model,
                store,
                request_id=request_id,
                index=index,
                total=total,
            )
            return StreamingResponse(generator, media_type=media_type)

        resp_dict = await call_antigravity_completion(
            upstream_model_id,
            api_key,
            request_kwargs,
            timeout=default_timeout,
        )
        return _record_success_and_respond(
            resp_dict,
            transformer,
            upstream_model_id,
            model,
            store,
            request_id=request_id,
            index=index,
            total=total,
            duration_sec=time.time() - attempt_start,
        )

    litellm_kwargs = map_to_litellm_kwargs(model, api_key, request_kwargs)
    litellm_kwargs["num_retries"] = 0
    litellm_kwargs.pop("stream", None)
    if "timeout" not in litellm_kwargs:
        litellm_kwargs["timeout"] = default_timeout

    if is_stream:
        response_gen = await acompletion(**litellm_kwargs, stream=True)
        media_type = "text/event-stream" if api_type == "anthropic" else "text/plain"
        generator = create_sse_stream_generator(
            response_gen,
            api_type,
            model,
            store,
            request_id=request_id,
            index=index,
            total=total,
        )
        return StreamingResponse(generator, media_type=media_type)

    response_obj = await acompletion(**litellm_kwargs)
    resp_dict = (
        response_obj.model_dump() if hasattr(response_obj, "model_dump") else dict(response_obj)
    )

    return _record_success_and_respond(
        resp_dict,
        transformer,
        upstream_model_id,
        model,
        store,
        request_id=request_id,
        index=index,
        total=total,
        duration_sec=time.time() - attempt_start,
    )


def _record_success_and_respond(
    resp_dict: dict[str, Any],
    transformer: Any,
    upstream_model_id: str,
    model: SleepyRouterModel,
    store: ConfigStore,
    *,
    request_id: int,
    index: int,
    total: int,
    duration_sec: float,
) -> Response:
    usage_data = resp_dict.get("usage") or {}
    in_tok = usage_data.get("prompt_tokens") or 0
    out_tok = usage_data.get("completion_tokens") or 0

    default_event_bus.publish(
        CandidateSucceededEvent(
            ts=time.time(),
            request_id=request_id,
            index=index,
            total=total,
            model_id=model.id,
            provider=model.provider,
            duration_sec=duration_sec,
            input_tokens=in_tok,
            output_tokens=out_tok,
        )
    )

    store.append_usage(
        UsageLogEntry(
            ts=datetime.datetime.now(datetime.UTC).isoformat(),
            model=model.usage_id or model.id,
            input_tokens=in_tok,
            output_tokens=out_tok,
            success=True,
        )
    )

    transformed_resp = transformer.transform_response(resp_dict, upstream_model_id)
    return JSONResponse(content=transformed_resp)


async def process_chat_candidates(
    store: ConfigStore,
    api_keys: ProviderAPIKeys,
    by_id: dict[str, SleepyRouterModel],
    candidates: list[str],
    body: dict[str, Any],
    api_type: str,
    *,
    request_id: int = 0,
    is_stream: bool = False,
) -> Response:
    overall_start = time.time()
    upstream_error = ""
    tried_any = False
    tried_models: list[str] = []
    total_cands = len(candidates)
    default_timeout = float(os.environ.get("UPSTREAM_TIMEOUT", "60.0"))

    for idx, model_id in enumerate(candidates):
        model = by_id.get(model_id)
        if not model:
            continue

        tried_any = True
        tried_models.append(model_id)
        api_key = api_key_for(api_keys, model.source)
        attempt_start = time.time()

        if not api_key:
            upstream_error = f"[{model_id}] API key missing for provider {model.source}"
            _emit_failure_and_failover(
                model,
                upstream_error,
                candidates,
                idx,
                by_id,
                request_id=request_id,
                total_cands=total_cands,
                duration_sec=0.0,
            )
            continue

        try:
            return await _execute_candidate_attempt(
                model,
                api_key,
                body,
                api_type,
                store,
                request_id=request_id,
                index=idx + 1,
                total=total_cands,
                is_stream=is_stream,
                default_timeout=default_timeout,
            )
        except Exception as e:  # noqa: BLE001
            duration_sec = time.time() - attempt_start
            err_msg = str(e)
            upstream_error = f"[{model_id}] {truncate(err_msg, 300)}"
            store.append_usage(
                UsageLogEntry(
                    ts=datetime.datetime.now(datetime.UTC).isoformat(),
                    model=model.usage_id or model.id,
                    input_tokens=0,
                    output_tokens=0,
                    success=False,
                )
            )
            _emit_failure_and_failover(
                model,
                err_msg,
                candidates,
                idx,
                by_id,
                request_id=request_id,
                total_cands=total_cands,
                duration_sec=duration_sec,
            )
            continue

    total_dur = time.time() - overall_start
    default_event_bus.publish(
        AllCandidatesFailedEvent(
            ts=time.time(),
            request_id=request_id,
            candidates_tried=tried_models,
            last_error=upstream_error,
            total_duration_sec=total_dur,
        )
    )

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


def _emit_failure_and_failover(
    model: SleepyRouterModel,
    error_message: str,
    candidates: list[str],
    idx: int,
    by_id: dict[str, SleepyRouterModel],
    *,
    request_id: int,
    total_cands: int,
    duration_sec: float,
) -> None:
    default_event_bus.publish(
        CandidateFailedEvent(
            ts=time.time(),
            request_id=request_id,
            index=idx + 1,
            total=total_cands,
            model_id=model.id,
            provider=model.provider,
            duration_sec=duration_sec,
            error_message=error_message,
        )
    )
    next_id = _next_valid_candidate(candidates, idx + 1, by_id)
    if next_id:
        default_event_bus.publish(
            FailoverEvent(
                ts=time.time(),
                request_id=request_id,
                index=idx + 1,
                total=total_cands,
                failed_model_id=model.id,
                next_model_id=next_id,
                provider=model.provider,
                error_message=error_message,
            )
        )


def _next_valid_candidate(
    candidates: list[str], start_idx: int, by_id: dict[str, SleepyRouterModel]
) -> str | None:
    """Return the next candidate model_id that exists in by_id, or None."""
    for i in range(start_idx, len(candidates)):
        if candidates[i] in by_id:
            return candidates[i]
    return None
