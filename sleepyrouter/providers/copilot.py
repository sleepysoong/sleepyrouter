import os
import time
from typing import Any

import requests

from sleepyrouter.types import SleepyRouterModel

from .base import BaseProviderAdapter, inject_max_reasoning

COPILOT_TOKEN_URL_DEFAULT = "https://api.github.com/copilot_internal/v2/token"
_copilot_token_cache: tuple[str, float] | None = None


def exchange_copilot_token(api_key: str) -> str:
    global _copilot_token_cache
    now = time.time()
    if _copilot_token_cache and now < _copilot_token_cache[1] - 300:
        return _copilot_token_cache[0]

    token_url = os.environ.get(
        "SLEEPYROUTER_COPILOT_TOKEN_URL", COPILOT_TOKEN_URL_DEFAULT
    )
    resp = requests.get(
        token_url,
        headers={
            "Authorization": f"token {api_key}",
            "User-Agent": "sleepyrouter/0.0.4",
        },
        timeout=10,
    )
    if not resp.ok:
        raise RuntimeError(f"copilot 토큰 교환 실패: {resp.status_code} {resp.reason}")
    data = resp.json()
    token_str = str(data.get("token", ""))
    expires_at = data.get("expires_at")
    if not token_str or not expires_at:
        raise RuntimeError("copilot 토큰 응답에 token 또는 expires_at 필드가 없어요")

    _copilot_token_cache = (token_str, float(expires_at))
    return token_str


class CopilotProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="Copilot",
            source="copilot",
            api_key_env_var="GITHUB_COPILOT_TOKEN",
            message_protocol="openai",
            default_reasoning_effort="high",
        )

    def prepare_api_key(self, api_key: str) -> str:
        return exchange_copilot_token(api_key)

    def map_litellm_kwargs(
        self, model: SleepyRouterModel, api_key: str, kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = model.upstream_id or model.id
        res = inject_max_reasoning(kwargs, effort="high")
        base_url = os.environ.get(
            "SLEEPYROUTER_COPILOT_BASE_URL", "https://api.githubcopilot.com"
        )
        res["model"] = f"openai/{upstream_id}"
        res["api_base"] = base_url
        res["api_key"] = api_key
        res["headers"] = {
            "Copilot-Integration-Id": "vscode-chat",
            "Editor-Version": "vscode/1.99.0",
            "Editor-Plugin-Version": "copilot-chat/0.26.7",
            "x-github-api-version": "2025-04-01",
        }
        return res
