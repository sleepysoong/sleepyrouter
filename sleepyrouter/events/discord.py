"""Webhook Observer for model failure and failover alerts (Discord, Slack, generic webhooks)."""

import asyncio
import os

import requests

from sleepyrouter.utils import get_config_root, read_local_env, truncate

from .bus import AllCandidatesFailedEvent, CandidateFailedEvent, FailoverEvent


def get_webhook_url() -> str:
    url = os.environ.get("DISCORD_WEBHOOK_URL") or os.environ.get("WEBHOOK_URL")
    if url:
        return url.strip()
    try:
        local_env = read_local_env(get_config_root())
        return (
            local_env.get("DISCORD_WEBHOOK_URL") or local_env.get("WEBHOOK_URL") or ""
        ).strip()
    except (OSError, RuntimeError):
        return ""


def _send_webhook(content: str) -> None:
    url = get_webhook_url()
    if not url:
        return

    payload = {
        "content": content,
        "text": content,
    }

    def _post() -> None:
        try:
            requests.post(
                url,
                json=payload,
                headers={"Content-Type": "application/json"},
                timeout=5,
            )
        except (requests.RequestException, OSError):
            pass

    try:
        loop = asyncio.get_running_loop()
        loop.create_task(asyncio.to_thread(_post))
    except RuntimeError:
        _post()


def notify_discord_on_failure(event: CandidateFailedEvent) -> None:
    err_text = truncate(event.error_message, 1700)
    content = (
        f"⚠️ **[SleepyRouter] Model Call Failed**\n"
        f"• **Model**: `{event.model_id}` ({event.provider})\n"
        f"• **Error**: ```\n{err_text}\n```"
    )
    _send_webhook(content)


def notify_discord_on_failover(event: FailoverEvent) -> None:
    err_text = truncate(event.error_message, 1600)
    content = (
        f"🔄 **[SleepyRouter] Failover to Next Model**\n"
        f"• **Failed**: `{event.failed_model_id}` ({event.provider})\n"
        f"• **Next Candidate**: `{event.next_model_id}`\n"
        f"• **Failure Detail**: ```\n{err_text}\n```"
    )
    _send_webhook(content)


def notify_discord_on_all_failed(event: AllCandidatesFailedEvent) -> None:
    tried = (
        ", ".join(f"`{m}`" for m in event.candidates_tried)
        if event.candidates_tried
        else "(none)"
    )
    err_text = truncate(event.last_error, 1600)
    content = (
        f"🚨 **[SleepyRouter] All Candidates Failed**\n"
        f"• **Tried Models**: {tried}\n"
        f"• **Last Error**: ```\n{err_text}\n```"
    )
    _send_webhook(content)
