"""Event publishing helpers for failover processing."""

import time

from sleepyrouter.events import (
    AllCandidatesFailedEvent,
    CandidateAttemptEvent,
    CandidateFailedEvent,
    CandidateSucceededEvent,
    FailoverEvent,
    default_event_bus,
)
from sleepyrouter.types import SleepyRouterModel


def emit_attempt_event(
    *,
    request_id: int,
    index: int,
    total: int,
    model: SleepyRouterModel,
    upstream_model_id: str,
) -> None:
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


def emit_success_event(
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


def emit_failure_and_failover(
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
    for next_idx in range(idx + 1, len(candidates)):
        next_cand = candidates[next_idx]
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


def emit_all_failed_event(
    *,
    request_id: int,
    candidates_tried: list[str],
    last_error: str,
    total_duration_sec: float,
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
