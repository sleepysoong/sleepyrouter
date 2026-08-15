import os
from typing import Any

from sleepyrouter.types import SleepyRouterModel

from .base import BaseProviderAdapter


class GoogleProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="Google",
            source="google",
            api_key_env_var="GOOGLE_API_KEY",
            message_protocol="openai",
        )

    def map_litellm_kwargs(
        self, model: SleepyRouterModel, api_key: str, kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = model.upstream_id or model.id
        res = dict(kwargs)
        base_url = os.environ.get("SLEEPYROUTER_GOOGLE_BASE_URL")
        if base_url:
            res["model"] = f"openai/{upstream_id}"
            res["api_base"] = base_url
        else:
            res["model"] = f"gemini/{upstream_id}"
        res["api_key"] = api_key
        return res
