import os
from typing import Any

from sleepyrouter.types import SleepyRouterModel

from .base import (
    MAX_REASONING_EFFORT_HIGH,
    MAX_THINKING_BUDGET,
    BaseProviderAdapter,
    inject_max_reasoning,
)


class GoogleProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="Google",
            source="google",
            api_key_env_var="GOOGLE_API_KEY",
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
        base_url = os.environ.get("SLEEPYROUTER_GOOGLE_BASE_URL")
        if base_url:
            res["model"] = f"openai/{upstream_id}"
            res["api_base"] = base_url
        else:
            res["model"] = f"gemini/{upstream_id}"
        res["api_key"] = api_key
        return res
