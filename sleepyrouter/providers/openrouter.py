import os
from typing import Any

from sleepyrouter.types import SleepyRouterModel

from .base import BaseProviderAdapter


class OpenRouterProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="OpenRouter",
            source="openrouter",
            api_key_env_var="OPENROUTER_API_KEY",
            message_protocol="anthropic",
        )

    def map_litellm_kwargs(
        self, model: SleepyRouterModel, api_key: str, kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = model.upstream_id or model.id
        res = dict(kwargs)
        res["model"] = f"openrouter/{upstream_id}"
        res["api_key"] = api_key
        base_url = os.environ.get("SLEEPYROUTER_OPENROUTER_BASE_URL")
        if base_url:
            res["api_base"] = base_url
        res["headers"] = {
            "HTTP-Referer": "https://github.com/sleepysoong/sleepyrouter",
            "X-OpenRouter-Title": "sleepyrouter",
        }
        return res
