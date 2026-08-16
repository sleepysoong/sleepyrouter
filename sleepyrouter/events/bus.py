"""Observer Event Bus pattern for sleepyrouter lifecycle events."""

import asyncio
from collections.abc import Callable
from dataclasses import dataclass
import inspect
from typing import Any


@dataclass
class ServerEvent:
    ts: float


@dataclass
class RequestReceivedEvent(ServerEvent):
    request_id: int
    method: str
    path: str
    requested_model: str


@dataclass
class CandidateAttemptEvent(ServerEvent):
    request_id: int
    model_id: str
    provider: str


@dataclass
class CandidateFailedEvent(ServerEvent):
    request_id: int
    model_id: str
    provider: str
    error_message: str


@dataclass
class ResponseSentEvent(ServerEvent):
    request_id: int
    status_code: int
    duration_ms: float


@dataclass
class FailoverEvent(ServerEvent):
    """Fired when a candidate fails and the router moves to the next model."""

    request_id: int
    failed_model_id: str
    next_model_id: str
    provider: str
    error_message: str


@dataclass
class AllCandidatesFailedEvent(ServerEvent):
    """Fired when every candidate has been exhausted with no success."""

    request_id: int
    candidates_tried: list[str]
    last_error: str


EventHandler = Callable[[Any], None]


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
