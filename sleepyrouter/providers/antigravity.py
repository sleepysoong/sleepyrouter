import os
from typing import Any

from sleepyrouter.types import SleepyRouterModel

from .base import (
    MAX_REASONING_EFFORT_HIGH,
    MAX_THINKING_BUDGET,
    BaseProviderAdapter,
    inject_max_reasoning,
)

ANTIGRAVITY_DEFAULT_BASE_URL = "https://cloudcode-pa.googleapis.com/v1"


class AntigravityProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="Google Antigravity",
            source="antigravity",
            api_key_env_var="ANTIGRAVITY_API_KEY",
            message_protocol="openai",
            default_reasoning_effort=MAX_REASONING_EFFORT_HIGH,
            default_thinking_budget=MAX_THINKING_BUDGET,
        )

    def map_litellm_kwargs(
        self, model: SleepyRouterModel, api_key: str, kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = model.upstream_id or model.id
        res = inject_max_reasoning(
            kwargs,
            effort=MAX_REASONING_EFFORT_HIGH,
            thinking_budget=MAX_THINKING_BUDGET,
        )
        base_url = os.environ.get(
            "SLEEPYROUTER_ANTIGRAVITY_BASE_URL",
            os.environ.get("ANTIGRAVITY_BASE_URL", ANTIGRAVITY_DEFAULT_BASE_URL),
        )
        res["model"] = f"openai/{upstream_id}"
        res["api_base"] = base_url
        res["api_key"] = api_key
        res["headers"] = {
            "User-Agent": "antigravity/1.0.0",
            "X-Goog-Api-Client": "gl-python/3.14.0",
        }
        return res
