"""LiteLLM provider mappings and execution helpers."""

import os
import time
from typing import Any

import litellm

from .types import SleepyRouterModel, source_of

# Disable litellm telemetry noise
litellm.telemetry = False
litellm.drop_params = True

COPILOT_TOKEN_URL_DEFAULT = "https://api.github.com/copilot_internal/v2/token"

_copilot_token_cache: tuple[str, float] | None = None


def exchange_copilot_token(api_key: str) -> str:
    global _copilot_token_cache
    now = time.time()
    if _copilot_token_cache and now < _copilot_token_cache[1] - 300:
        return _copilot_token_cache[0]

    import requests

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
    token = data.get("token")
    expires_at = data.get("expires_at")
    if not token or not expires_at:
        raise RuntimeError("copilot 토큰 응답에 token 또는 expires_at 필드가 없어요")

    _copilot_token_cache = (token, float(expires_at))
    return token


def map_to_litellm_kwargs(
    model: SleepyRouterModel,
    api_key: str,
    kwargs: dict[str, Any],
) -> dict[str, Any]:
    """Map sleepyrouter model and provider source to LiteLLM model string and API params."""
    source = source_of(model)
    upstream_id = model.upstream_id or model.id

    litellm_kwargs = dict(kwargs)

    if source == "openrouter":
        litellm_kwargs["model"] = f"openrouter/{upstream_id}"
        litellm_kwargs["api_key"] = api_key
        base_url = os.environ.get("SLEEPYROUTER_OPENROUTER_BASE_URL")
        if base_url:
            litellm_kwargs["api_base"] = base_url
        litellm_kwargs["headers"] = {
            "HTTP-Referer": "https://github.com/sleepysoong/sleepyrouter",
            "X-OpenRouter-Title": "sleepyrouter",
        }

    elif source == "nvidia":
        base_url = os.environ.get(
            "SLEEPYROUTER_NVIDIA_BASE_URL", "https://integrate.api.nvidia.com/v1"
        )
        litellm_kwargs["model"] = f"openai/{upstream_id}"
        litellm_kwargs["api_base"] = base_url
        litellm_kwargs["api_key"] = api_key

    elif source == "copilot":
        session_token = exchange_copilot_token(api_key)
        base_url = os.environ.get(
            "SLEEPYROUTER_COPILOT_BASE_URL", "https://api.githubcopilot.com"
        )
        litellm_kwargs["model"] = f"openai/{upstream_id}"
        litellm_kwargs["api_base"] = base_url
        litellm_kwargs["api_key"] = session_token
        litellm_kwargs["headers"] = {
            "Copilot-Integration-Id": "vscode-chat",
            "Editor-Version": "vscode/1.99.0",
            "Editor-Plugin-Version": "copilot-chat/0.26.7",
            "x-github-api-version": "2025-04-01",
        }

    elif source == "google":
        base_url = os.environ.get("SLEEPYROUTER_GOOGLE_BASE_URL")
        if base_url:
            litellm_kwargs["model"] = f"openai/{upstream_id}"
            litellm_kwargs["api_base"] = base_url
        else:
            litellm_kwargs["model"] = f"gemini/{upstream_id}"
        litellm_kwargs["api_key"] = api_key

    elif source == "zen":
        base_url = os.environ.get("SLEEPYROUTER_ZEN_BASE_URL", "https://api.zen.dev/v1")
        litellm_kwargs["model"] = f"openai/{upstream_id}"
        litellm_kwargs["api_base"] = base_url
        litellm_kwargs["api_key"] = api_key

    else:
        # Fallback for arbitrary custom provider sources
        litellm_kwargs["model"] = f"openai/{upstream_id}"
        litellm_kwargs["api_key"] = api_key

    return litellm_kwargs
