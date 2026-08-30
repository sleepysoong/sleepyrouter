"""Candidate failover and retry processing via unified ProviderAdapter interface."""

import datetime
import os
import time
from typing import Any

from fastapi import Response
from fastapi.responses import JSONResponse

from sleepyrouter.config import ConfigStore
from sleepyrouter.events import (
    AllCandidatesFailedEvent,
    CandidateAttemptEvent,
    CandidateFailedEvent,
    CandidateSucceededEvent,
    FailoverEvent,
    default_event_bus,
)
from sleepyrouter.providers import (
    BaseProviderAdapter,
    api_key_for,
    default_provider_registry,
)
from sleepyrouter.types import SleepyRouterModel, UsageLogEntry
from sleepyrouter.utils import format_error_message, truncate

from .stream import start_streaming_response


def transform_request(body: dict[str, Any], model_id: str) -> dict[str, Any]:
    """Sets the target model field on the request payload."""
    res = dict(body)
    res["model"] = model_id
    return res



def _emit_attempt(
    *, request_id: int, index: int, total: int, model: SleepyRouterModel, upstream_id: str
) -> None:
    default_event_bus.publish(
        CandidateAttemptEvent(
            ts=time.time(),
            request_id=request_id,
            index=index,
            total=total,
            model_id=model.id,
            provider=model.provider,
            upstream_id=upstream_id,
        )
    )


def _emit_success(
    *,
    request_id: int,
    index: int,
    total: int,
    model: SleepyRouterModel,
    duration_sec: float,
    input_tokens: int,
    output_tokens: int,
) -> None:
    default_event_bus.publish(
        CandidateSucceededEvent(
            ts=time.time(),
            request_id=request_id,
            index=index,
            total=total,
            model_id=model.id,
            provider=model.provider,
            duration_sec=duration_sec,
            input_tokens=input_tokens,
            output_tokens=output_tokens,
        )
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
    for next_cand in candidates[idx + 1 :]:
        if next_cand in by_id:
            default_event_bus.publish(
                FailoverEvent(
                    ts=time.time(),
                    request_id=request_id,
                    index=idx + 1,
                    total=total_cands,
                    failed_model_id=model.id,
                    next_model_id=next_cand,
                    provider=model.provider,
                    error_message=error_message,
                )
            )
            break


def _emit_all_failed(
    *, request_id: int, candidates_tried: list[str], last_error: str, total_duration_sec: float
) -> None:
    default_event_bus.publish(
        AllCandidatesFailedEvent(
            ts=time.time(),
            request_id=request_id,
            candidates_tried=candidates_tried,
            last_error=last_error,
            total_duration_sec=total_duration_sec,
        )
    )


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

    _emit_success(
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

    _emit_attempt(
        request_id=request_id,
        index=index,
        total=total,
        model=model,
        upstream_id=upstream_model_id,
    )

    adapter = default_provider_registry.get(model.source) or BaseProviderAdapter(
        name=model.provider,
        source=model.source,
        api_key_env_var="",
    )

    if is_stream:
        gen = await adapter.stream(
            model,
            api_key,
            request_kwargs,
            timeout=default_timeout,
        )
        return await start_streaming_response(
            gen,
            model,
            store,
            request_id=request_id,
            index=index,
            total=total,
        )

    resp_dict = await adapter.complete(
        model,
        api_key,
        request_kwargs,
        timeout=default_timeout,
    )
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

    total_dur = time.time() - overall_start
    _emit_all_failed(
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
