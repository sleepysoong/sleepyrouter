"""Copilot provider adapter with thread/coroutine-safe token caching."""

import asyncio
import threading
import time

import httpx

from .base import BaseProviderAdapter

COPILOT_TOKEN_ENDPOINT = "https://api.github.com/copilot_internal/v2/token"  # noqa: S105
COPILOT_BASE_URL = "https://api.githubcopilot.com"


class CopilotTokenCache:
    """Token cache for GitHub Copilot with thread and coroutine safety."""

    def __init__(self) -> None:
        self.token: str | None = None
        self.expires_at: float = 0.0
        self._thread_lock = threading.Lock()
        self._async_lock = asyncio.Lock()

    def get_token(self, api_key: str) -> str:
        now = time.time()
        with self._thread_lock:
            if self.token and now < self.expires_at - 300:
                return self.token

            with httpx.Client(timeout=10.0) as client:
                resp = client.get(
                    COPILOT_TOKEN_ENDPOINT,
                    headers={
                        "Authorization": f"token {api_key}",
                        "User-Agent": "sleepyrouter/1.0.0",
                    },
                )
                if resp.status_code != 200:
                    err_msg = f"copilot 토큰 교환 실패: {resp.status_code} {resp.reason_phrase}"
                    raise RuntimeError(err_msg)
                data = resp.json()
                token_str = str(data.get("token", ""))
                expires_at = data.get("expires_at")
                if not token_str or not expires_at:
                    err_empty = "copilot 토큰 응답에 token 또는 expires_at 필드가 없어요"
                    raise RuntimeError(err_empty)

                self.token = token_str
                self.expires_at = float(expires_at)
                return token_str

    async def async_get_token(self, api_key: str) -> str:
        now = time.time()
        if self.token and now < self.expires_at - 300:
            return self.token

        async with self._async_lock:
            if self.token and now < self.expires_at - 300:
                return self.token

            async with httpx.AsyncClient(timeout=10.0) as client:
                resp = await client.get(
                    COPILOT_TOKEN_ENDPOINT,
                    headers={
                        "Authorization": f"token {api_key}",
                        "User-Agent": "sleepyrouter/1.0.0",
                    },
                )
                if resp.status_code != 200:
                    err_msg = f"copilot 토큰 교환 실패: {resp.status_code} {resp.reason_phrase}"
                    raise RuntimeError(err_msg)
                data = resp.json()
                token_str = str(data.get("token", ""))
                expires_at = data.get("expires_at")
                if not token_str or not expires_at:
                    err_empty = "copilot 토큰 응답에 token 또는 expires_at 필드가 없어요"
                    raise RuntimeError(err_empty)

                self.token = token_str
                self.expires_at = float(expires_at)
                return token_str


_default_token_cache = CopilotTokenCache()


def exchange_copilot_token(api_key: str) -> str:
    return _default_token_cache.get_token(api_key)


async def async_exchange_copilot_token(api_key: str) -> str:
    return await _default_token_cache.async_get_token(api_key)


class CopilotProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="Copilot",
            source="copilot",
            api_key_env_var="GITHUB_COPILOT_TOKEN",
            message_protocol="openai",
            api_base=COPILOT_BASE_URL,
            extra_headers={
                "Copilot-Integration-Id": "vscode-chat",
                "Editor-Version": "vscode/1.99.0",
                "Editor-Plugin-Version": "copilot-chat/0.26.7",
                "x-github-api-version": "2025-04-01",
            },
        )

    def prepare_api_key(self, api_key: str) -> str:
        return exchange_copilot_token(api_key)
