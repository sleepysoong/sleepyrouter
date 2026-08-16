"""Console logging observer for tree-structured lifecycle output without emojis."""

import logging
import sys

from .bus import (
    AllCandidatesFailedEvent,
    CandidateAttemptEvent,
    CandidateFailedEvent,
    CandidatesResolvedEvent,
    CandidateSucceededEvent,
    FailoverEvent,
    RequestReceivedEvent,
)

logger = logging.getLogger("sleepyrouter")
if not logger.handlers:
    handler = logging.StreamHandler(sys.stdout)
    formatter = logging.Formatter(
        fmt="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%H:%M:%S",
    )
    handler.setFormatter(formatter)
    logger.addHandler(handler)
    logger.setLevel(logging.INFO)
    logger.propagate = False


def log_request_received(event: RequestReceivedEvent) -> None:
    stream_str = " (stream=True)" if event.is_stream else ""
    logger.info(
        "[REQ #%d] %s %s | model='%s'%s",
        event.request_id,
        event.method,
        event.path,
        event.requested_model,
        stream_str,
    )


def log_candidates_resolved(event: CandidatesResolvedEvent) -> None:
    c_chain = " -> ".join(event.candidates)
    logger.info(
        "[REQ #%d] ├── candidates (%d) [%s]: %s",
        event.request_id,
        len(event.candidates),
        event.route_reason,
        c_chain,
    )


def log_candidate_attempt(event: CandidateAttemptEvent) -> None:
    upstream_info = (
        f" (upstream: {event.upstream_id})"
        if event.upstream_id and event.upstream_id != event.model_id
        else ""
    )
    logger.info(
        "[REQ #%d] ├── [%d/%d] %s%s -> calling...",
        event.request_id,
        event.index,
        event.total,
        event.model_id,
        upstream_info,
    )


def log_candidate_succeeded(event: CandidateSucceededEvent) -> None:
    logger.info(
        "[REQ #%d] │   └── [OK] succeeded in %.2fs (in: %d tok, out: %d tok)",
        event.request_id,
        event.duration_sec,
        event.input_tokens,
        event.output_tokens,
    )


def log_candidate_failed(event: CandidateFailedEvent) -> None:
    logger.error(
        "[REQ #%d] │   └── [FAIL] failed in %.2fs: %s",
        event.request_id,
        event.duration_sec,
        event.error_message,
    )


def log_failover(event: FailoverEvent) -> None:
    logger.warning(
        "[REQ #%d] │   └── [FAILOVER] -> next candidate '%s'",
        event.request_id,
        event.next_model_id,
    )


def log_all_candidates_failed(event: AllCandidatesFailedEvent) -> None:
    c_list = ", ".join(f"'{c}'" for c in event.candidates_tried)
    logger.critical(
        "[REQ #%d] └── [EXHAUSTED] all %d candidates failed in %.2fs: [%s] | last error: %s",
        event.request_id,
        len(event.candidates_tried),
        event.total_duration_sec,
        c_list,
        event.last_error,
    )
