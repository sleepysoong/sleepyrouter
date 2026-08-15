import os
from typing import Any

from sleepyrouter.types import SleepyRouterModel

from .base import BaseProviderAdapter


class NVIDIAProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="NVIDIA",
            source="nvidia",
            api_key_env_var="NVIDIA_API_KEY",
            message_protocol="openai",
        )

    def map_litellm_kwargs(
        self, model: SleepyRouterModel, api_key: str, kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = model.upstream_id or model.id
        res = dict(kwargs)
        base_url = os.environ.get(
            "SLEEPYROUTER_NVIDIA_BASE_URL", "https://integrate.api.nvidia.com/v1"
        )
        res["model"] = f"openai/{upstream_id}"
        res["api_base"] = base_url
        res["api_key"] = api_key
        return res
