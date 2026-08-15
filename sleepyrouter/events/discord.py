"""Discord Webhook Observer for candidate failure alerts."""

import os

import requests

from sleepyrouter.utils import truncate

from .bus import CandidateFailedEvent


def notify_discord_on_failure(event: CandidateFailedEvent) -> None:
    url = os.environ.get("DISCORD_WEBHOOK_URL")
    if not url:
        return

    content = (
        f"Upstream failure [{event.model_id}]: {truncate(event.error_message, 1800)}"
    )
    try:
        requests.post(
            url,
            json={"content": content},
            headers={"Content-Type": "application/json"},
            timeout=5,
        )
    except (requests.RequestException, OSError):
        pass
