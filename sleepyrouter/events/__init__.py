from .bus import (
    AllCandidatesFailedEvent,
    CandidateAttemptEvent,
    CandidateFailedEvent,
    CandidatesResolvedEvent,
    CandidateSucceededEvent,
    EventBus,
    FailoverEvent,
    RequestReceivedEvent,
    ResponseSentEvent,
    ServerEvent,
    default_event_bus,
)
from .console import (
    log_all_candidates_failed,
    log_candidate_attempt,
    log_candidate_failed,
    log_candidate_succeeded,
    log_candidates_resolved,
    log_failover,
    log_request_received,
)
from .discord import (
    notify_discord_on_all_failed,
    notify_discord_on_failover,
    notify_discord_on_failure,
)

# Register Console logging observers on default bus
default_event_bus.subscribe(RequestReceivedEvent, log_request_received)
default_event_bus.subscribe(CandidatesResolvedEvent, log_candidates_resolved)
default_event_bus.subscribe(CandidateAttemptEvent, log_candidate_attempt)
default_event_bus.subscribe(CandidateSucceededEvent, log_candidate_succeeded)
default_event_bus.subscribe(CandidateFailedEvent, log_candidate_failed)
default_event_bus.subscribe(FailoverEvent, log_failover)
default_event_bus.subscribe(AllCandidatesFailedEvent, log_all_candidates_failed)

# Register Discord webhook observers on default bus
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
