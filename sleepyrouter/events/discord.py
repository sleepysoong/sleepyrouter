"""Webhook Observer for model failure and failover alerts (Discord, Slack, generic webhooks)."""

import asyncio
import os
from typing import Any

import httpx

from sleepyrouter.utils import get_config_root, read_local_env, truncate

from .bus import AllCandidatesFailedEvent, CandidateFailedEvent, FailoverEvent

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
        local_env = read_local_env(get_config_root())
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

    payload = {
        "content": content,
        "text": content,
    }

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
