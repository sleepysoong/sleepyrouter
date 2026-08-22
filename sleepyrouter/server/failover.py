"""Candidate failover and retry processing via LiteLLM and Antigravity."""

import datetime
import os
import time
from typing import Any

from fastapi import Response
from fastapi.responses import JSONResponse
from litellm import acompletion

from sleepyrouter.config import ConfigStore
from sleepyrouter.protocol import transform_request
from sleepyrouter.providers import api_key_for, map_to_litellm_kwargs
from sleepyrouter.providers.antigravity import (
    call_antigravity_completion,
    call_antigravity_stream,
)
from sleepyrouter.types import SleepyRouterModel, UsageLogEntry
from sleepyrouter.utils import format_error_message, truncate

from .failover_events import (
    emit_all_failed_event,
    emit_attempt_event,
    emit_failure_and_failover,
    emit_success_event,
)
from .stream import start_streaming_response


def _record_success_and_respond(
    resp_dict: dict[str, Any],
    model: SleepyRouterModel,
    store: ConfigStore,
    *,
    request_id: int,
    index: int,
    total: int,
    duration_sec: float,
) -> Response:
    usage = resp_dict.get("usage") or {}
    in_tok = usage.get("prompt_tokens") or 0
    out_tok = usage.get("completion_tokens") or 0

    emit_success_event(
        request_id=request_id,
        index=index,
        total=total,
        model=model,
        duration_sec=duration_sec,
        input_tokens=in_tok,
        output_tokens=out_tok,
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
    return JSONResponse(content=resp_dict)


async def _execute_candidate_attempt(
    model: SleepyRouterModel,
    api_key: str,
    body: dict[str, Any],
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
    request_kwargs = transform_request(body, upstream_model_id)

    emit_attempt_event(
        request_id=request_id,
        index=index,
        total=total,
        model=model,
        upstream_model_id=upstream_model_id,
    )
    is_direct_antigravity = model.source == "antigravity" and not model.api_base

    if is_stream:
        if is_direct_antigravity:
            gen = call_antigravity_stream(
                upstream_model_id, api_key, request_kwargs, timeout=default_timeout
            )
        else:
            kwargs = map_to_litellm_kwargs(model, api_key, request_kwargs)
            kwargs["num_retries"] = 0
            kwargs.pop("stream", None)
            kwargs.setdefault("timeout", default_timeout)
            kwargs["stream_options"] = {"include_usage": True}
            gen = await acompletion(**kwargs, stream=True)

        return await start_streaming_response(
            gen,
            model,
            store,
            request_id=request_id,
            index=index,
            total=total,
        )

    if is_direct_antigravity:
        resp_dict = await call_antigravity_completion(
            upstream_model_id, api_key, request_kwargs, timeout=default_timeout
        )
    else:
        kwargs = map_to_litellm_kwargs(model, api_key, request_kwargs)
        kwargs["num_retries"] = 0
        kwargs.pop("stream", None)
        kwargs.setdefault("timeout", default_timeout)
        resp_obj = await acompletion(**kwargs)
        resp_dict = resp_obj.model_dump() if hasattr(resp_obj, "model_dump") else dict(resp_obj)

    return _record_success_and_respond(
        resp_dict,
        model,
        store,
        request_id=request_id,
        index=index,
        total=total,
        duration_sec=time.time() - attempt_start,
    )


async def process_chat_candidates(
    store: ConfigStore,
    by_id: dict[str, SleepyRouterModel],
    candidates: list[str],
    body: dict[str, Any],
    *,
    request_id: int = 0,
    is_stream: bool = False,
    env: dict[str, str] | None = None,
) -> Response:
    overall_start = time.time()
    upstream_error = ""
    tried_any = False
    tried_models: list[str] = []
    total_cands = len(candidates)
    default_timeout = float(os.environ.get("UPSTREAM_TIMEOUT", "20.0"))

    for idx, model_id in enumerate(candidates):
        model = by_id.get(model_id)
        if not model:
            continue

        tried_any = True
        tried_models.append(model_id)
        api_key = api_key_for(model.source, env)
        attempt_start = time.time()

        if not api_key:
            upstream_error = f"[{model_id}] API key missing for provider {model.source}"
            emit_failure_and_failover(
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
                store,
                request_id=request_id,
                index=idx + 1,
                total=total_cands,
                is_stream=is_stream,
                default_timeout=default_timeout,
            )
        except Exception as e:  # noqa: BLE001
            duration_sec = time.time() - attempt_start
            err_msg = format_error_message(e)
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
            emit_failure_and_failover(
                model,
                err_msg,
                candidates,
                idx,
                by_id,
                request_id=request_id,
                total_cands=total_cands,
                duration_sec=duration_sec,
            )

    total_dur = time.time() - overall_start
    emit_all_failed_event(
        request_id=request_id,
        candidates_tried=tried_models,
        last_error=upstream_error,
        total_duration_sec=total_dur,
    )
    msg = (
        "선택된 모든 무료 모델이 실패했어요."
        if tried_any
        else "사용 가능한 모델이 없어요. API 키를 확인하세요."
    )
    return JSONResponse(
        status_code=502,
        content={"error": {"message": msg, "details": upstream_error}},
    )
