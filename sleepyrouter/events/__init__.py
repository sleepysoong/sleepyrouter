from .bus import (
    CandidateAttemptEvent,
    CandidateFailedEvent,
    EventBus,
    RequestReceivedEvent,
    ResponseSentEvent,
    ServerEvent,
    default_event_bus,
)
from .discord import notify_discord_on_failure

# Register Discord observer on default bus
default_event_bus.subscribe(CandidateFailedEvent, notify_discord_on_failure)

__all__ = [
    "CandidateAttemptEvent",
    "CandidateFailedEvent",
    "EventBus",
    "RequestReceivedEvent",
    "ResponseSentEvent",
    "ServerEvent",
    "default_event_bus",
    "notify_discord_on_failure",
]
