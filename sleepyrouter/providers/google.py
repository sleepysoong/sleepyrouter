from typing import Any

from sleepyrouter.types import SleepyRouterModel

from .base import (
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
            default_reasoning_effort="high",
        )

    def map_litellm_kwargs(
        self, model: SleepyRouterModel, api_key: str, kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = model.upstream_id or model.id
        res = inject_max_reasoning(
            kwargs,
            effort="high",
        )
        # Prevent LiteLLM conflict on Gemini models when reasoning_effort is set
        res.pop("thinking", None)
        res["model"] = f"gemini/{upstream_id}"
        res["api_key"] = api_key
        return res
