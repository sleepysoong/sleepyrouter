from .bus import (
    AllCandidatesFailedEvent,
    CandidateAttemptEvent,
    CandidateFailedEvent,
    EventBus,
    FailoverEvent,
    RequestReceivedEvent,
    ResponseSentEvent,
    ServerEvent,
    default_event_bus,
)
from .discord import notify_discord_on_failure, notify_discord_on_failover, notify_discord_on_all_failed

# Register Discord observers on default bus
default_event_bus.subscribe(CandidateFailedEvent, notify_discord_on_failure)
default_event_bus.subscribe(FailoverEvent, notify_discord_on_failover)
default_event_bus.subscribe(AllCandidatesFailedEvent, notify_discord_on_all_failed)

__all__ = [
    "AllCandidatesFailedEvent",
    "CandidateAttemptEvent",
    "CandidateFailedEvent",
    "EventBus",
    "FailoverEvent",
    "RequestReceivedEvent",
    "ResponseSentEvent",
    "ServerEvent",
    "default_event_bus",
    "notify_discord_on_failure",
    "notify_discord_on_failover",
    "notify_discord_on_all_failed",
]
