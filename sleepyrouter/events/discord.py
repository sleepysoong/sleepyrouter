"""Discord Webhook Observer for routing alerts."""

import os

import requests

from sleepyrouter.utils import truncate

from .bus import AllCandidatesFailedEvent, CandidateFailedEvent, FailoverEvent


def _send_discord(content: str) -> None:
    url = os.environ.get("DISCORD_WEBHOOK_URL")
    if not url:
        return
    try:
        requests.post(
            url,
            json={"content": content},
            headers={"Content-Type": "application/json"},
            timeout=5,
        )
    except (requests.RequestException, OSError):
        pass


def notify_discord_on_failure(event: CandidateFailedEvent) -> None:
    content = (
        f"\u26a0\ufe0f **Candidate Failed** [{event.model_id}]\n"
        f"> {truncate(event.error_message, 1800)}"
    )
    _send_discord(content)


def notify_discord_on_failover(event: FailoverEvent) -> None:
    content = (
        f"\U0001f504 **Failover** [{event.failed_model_id}] \u2192 [{event.next_model_id}]\n"
        f"> {truncate(event.error_message, 1800)}"
    )
    _send_discord(content)


def notify_discord_on_all_failed(event: AllCandidatesFailedEvent) -> None:
    tried = ", ".join(event.candidates_tried) if event.candidates_tried else "(none)"
    content = (
        f"\U0001f6a8 **All Candidates Failed** \u2014 routing exhausted\n"
        f"> Tried: {truncate(tried, 500)}\n"
        f"> Last error: {truncate(event.last_error, 1200)}"
    )
    _send_discord(content)
