"""Freebuff (Codebuff) provider adapter."""

from typing import Any

from sleepyrouter.types import SleepyRouterModel

from .base import BaseProviderAdapter, inject_max_reasoning

FREEBUFF_BASE_URL = "https://codebuff.com/api/v1"


class FreebuffProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="Freebuff",
            source="freebuff",
            api_key_env_var="FREEBUFF_API_KEY",
            message_protocol="openai",
            default_reasoning_effort="high",
        )

    def map_litellm_kwargs(
        self, model: SleepyRouterModel, api_key: str, kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = model.upstream_id or model.id
        res = inject_max_reasoning(kwargs, effort="high")
        res["model"] = f"openai/{upstream_id}"
        res["api_base"] = FREEBUFF_BASE_URL
        res["api_key"] = api_key
        res["headers"] = {
            "User-Agent": "freebuff/1.0.0",
        }
        return res
