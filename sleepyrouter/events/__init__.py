"""Events, lifecycle event bus, console logging, and Discord webhook notifications."""

import asyncio
from collections.abc import Callable
from dataclasses import dataclass, field
import inspect
import logging
import os
import sys
from typing import Any

import httpx

from sleepyrouter.utils import get_config_root, read_local_env, truncate

# ---------------------------------------------------------
# Event Dataclasses
# ---------------------------------------------------------


@dataclass
class ServerEvent:
    ts: float


@dataclass
class RequestReceivedEvent(ServerEvent):
    request_id: int
    method: str
    path: str
    requested_model: str
    is_stream: bool = False


@dataclass
class CandidatesResolvedEvent(ServerEvent):
    request_id: int
    requested_model: str
    candidates: list[str] = field(default_factory=list)
    route_reason: str = ""


@dataclass
class CandidateAttemptEvent(ServerEvent):
    request_id: int
    index: int
    total: int
    model_id: str
    provider: str
    upstream_id: str = ""


@dataclass
class CandidateSucceededEvent(ServerEvent):
    request_id: int
    index: int
    total: int
    model_id: str
    provider: str
    duration_sec: float = 0.0
    input_tokens: int = 0
    output_tokens: int = 0


@dataclass
class CandidateFailedEvent(ServerEvent):
    request_id: int
    model_id: str
    provider: str
    error_message: str
    index: int = 1
    total: int = 1
    duration_sec: float = 0.0


@dataclass
class ResponseSentEvent(ServerEvent):
    request_id: int
    status_code: int
    duration_ms: float


@dataclass
class FailoverEvent(ServerEvent):
    request_id: int
    failed_model_id: str
    next_model_id: str
    provider: str
    error_message: str
    index: int = 1
    total: int = 1


@dataclass
class AllCandidatesFailedEvent(ServerEvent):
    request_id: int
    candidates_tried: list[str]
    last_error: str
    total_duration_sec: float = 0.0


EventHandler = Callable[[Any], None]

# ---------------------------------------------------------
# EventBus
# ---------------------------------------------------------


class EventBus:
    def __init__(self) -> None:
        self._handlers: dict[type[ServerEvent], list[EventHandler]] = {}
        self._background_tasks: set[asyncio.Task[Any]] = set()

    def subscribe(self, event_type: type[ServerEvent], handler: EventHandler) -> None:
        if event_type not in self._handlers:
            self._handlers[event_type] = []
        self._handlers[event_type].append(handler)

    def publish(self, event: ServerEvent) -> None:
        handlers = self._handlers.get(type(event), [])
        for handler in handlers:
            try:
                if inspect.iscoroutinefunction(handler):
                    task = asyncio.create_task(handler(event))
                    self._background_tasks.add(task)
                    task.add_done_callback(self._background_tasks.discard)
                else:
                    handler(event)
            except (RuntimeError, ValueError, TypeError, OSError):
                pass


default_event_bus = EventBus()

# ---------------------------------------------------------
# Console Logging Observers
# ---------------------------------------------------------

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

logging.getLogger("uvicorn.access").disabled = True
logging.getLogger("uvicorn.access").propagate = False
logging.getLogger("uvicorn.access").setLevel(logging.WARNING)


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
    logger.info(
        "[REQ #%d] ├── candidates (%d) [%s]: %s",
        event.request_id,
        len(event.candidates),
        event.route_reason,
        " -> ".join(event.candidates),
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


# ---------------------------------------------------------
# Webhook Observers
# ---------------------------------------------------------

_background_tasks: set[asyncio.Task[Any]] = set()


class WebhookClientManager:
    def __init__(self) -> None:
        self._client: httpx.AsyncClient | None = None

    def get_client(self) -> httpx.AsyncClient:
        if self._client is None or self._client.is_closed:
            self._client = httpx.AsyncClient(
                timeout=5.0,
                limits=httpx.Limits(max_keepalive_connections=10, max_connections=50),
            )
        return self._client


_webhook_manager = WebhookClientManager()


def get_webhook_client() -> httpx.AsyncClient:
    return _webhook_manager.get_client()


def get_webhook_url() -> str:
    url = os.environ.get("DISCORD_WEBHOOK_URL") or os.environ.get("WEBHOOK_URL")
    if url:
        return url.strip()
    try:
        mod = sys.modules.get("sleepyrouter.events.discord")
        root_fn = getattr(mod, "get_config_root", get_config_root) if mod else get_config_root
        local_env = read_local_env(root_fn())
        return (local_env.get("DISCORD_WEBHOOK_URL") or local_env.get("WEBHOOK_URL") or "").strip()
    except (OSError, RuntimeError):
        return ""


async def _async_post_webhook(url: str, payload: dict[str, Any]) -> None:
    try:
        client = get_webhook_client()
        await client.post(url, json=payload)
    except (httpx.HTTPError, OSError):
        pass


def _send_webhook(content: str) -> None:
    url = get_webhook_url()
    if not url:
        return

    payload = {"content": content, "text": content}
    try:
        loop = asyncio.get_running_loop()
        task = loop.create_task(_async_post_webhook(url, payload))
        _background_tasks.add(task)
        task.add_done_callback(_background_tasks.discard)
    except RuntimeError:
        try:
            with httpx.Client(timeout=3.0) as sync_client:
                sync_client.post(url, json=payload)
        except (httpx.HTTPError, OSError):
            pass


def notify_discord_on_failure(event: CandidateFailedEvent) -> None:
    err_text = truncate(event.error_message, 1700)
    content = (
        f"⚠️ **[SleepyRouter] 모델 호출 실패**\n"
        f"• **대상 모델**: `{event.model_id}` (제공자: `{event.provider}`)\n"
        f"• **오류 내용**: ```\n{err_text}\n```"
    )
    _send_webhook(content)


def notify_discord_on_failover(event: FailoverEvent) -> None:
    err_text = truncate(event.error_message, 1600)
    content = (
        f"🔄 **[SleepyRouter] 모델 호출 실패 → 다음 후보 모델 전환**\n"
        f"• **실패 모델**: `{event.failed_model_id}` (제공자: `{event.provider}`)\n"
        f"• **다음 시도 모델**: `{event.next_model_id}`\n"
        f"• **실패 상세 내용**: ```\n{err_text}\n```"
    )
    _send_webhook(content)


def notify_discord_on_all_failed(event: AllCandidatesFailedEvent) -> None:
    tried = (
        ", ".join(f"`{m}`" for m in event.candidates_tried) if event.candidates_tried else "(없음)"
    )
    err_text = truncate(event.last_error, 1600)
    content = (
        f"🚨 **[SleepyRouter] 모든 후보 모델 호출 실패**\n"
        f"• **시도한 모델 목록**: {tried}\n"
        f"• **최종 오류 내용**: ```\n{err_text}\n```"
    )
    _send_webhook(content)


# Subscribe defaults
default_event_bus.subscribe(RequestReceivedEvent, log_request_received)
default_event_bus.subscribe(CandidatesResolvedEvent, log_candidates_resolved)
default_event_bus.subscribe(CandidateAttemptEvent, log_candidate_attempt)
default_event_bus.subscribe(CandidateSucceededEvent, log_candidate_succeeded)
default_event_bus.subscribe(CandidateFailedEvent, log_candidate_failed)
default_event_bus.subscribe(FailoverEvent, log_failover)
default_event_bus.subscribe(AllCandidatesFailedEvent, log_all_candidates_failed)

default_event_bus.subscribe(CandidateFailedEvent, notify_discord_on_failure)
default_event_bus.subscribe(FailoverEvent, notify_discord_on_failover)
default_event_bus.subscribe(AllCandidatesFailedEvent, notify_discord_on_all_failed)

__all__ = [
    "AllCandidatesFailedEvent",
    "CandidateAttemptEvent",
    "CandidateFailedEvent",
    "CandidateSucceededEvent",
    "CandidatesResolvedEvent",
    "EventBus",
    "FailoverEvent",
    "RequestReceivedEvent",
    "ResponseSentEvent",
    "ServerEvent",
    "default_event_bus",
    "get_config_root",
    "get_webhook_client",
    "get_webhook_url",
    "log_all_candidates_failed",
    "log_candidate_attempt",
    "log_candidate_failed",
    "log_candidate_succeeded",
    "log_candidates_resolved",
    "log_failover",
    "log_request_received",
    "notify_discord_on_all_failed",
    "notify_discord_on_failover",
    "notify_discord_on_failure",
]
