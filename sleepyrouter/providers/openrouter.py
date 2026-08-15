from typing import Any

from sleepyrouter.types import SleepyRouterModel

from .base import (
    BaseProviderAdapter,
    inject_max_reasoning,
)


class OpenRouterProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="OpenRouter",
            source="openrouter",
            api_key_env_var="OPENROUTER_API_KEY",
            message_protocol="anthropic",
            default_reasoning_effort="xhigh",
            default_thinking_budget=32000,
        )

    def map_litellm_kwargs(
        self, model: SleepyRouterModel, api_key: str, kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = model.upstream_id or model.id
        res = inject_max_reasoning(
            kwargs,
            effort="xhigh",
            thinking_budget=32000,
        )
        res["model"] = f"openrouter/{upstream_id}"
        res["api_key"] = api_key
        res["api_base"] = "https://openrouter.ai/api/v1"
        res["headers"] = {
            "HTTP-Referer": "https://github.com/sleepysoong/sleepyrouter",
            "X-OpenRouter-Title": "sleepyrouter",
        }
        return res
