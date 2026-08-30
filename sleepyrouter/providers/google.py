"""Google Gemini provider adapter using official OpenAI-compatible endpoint."""

from pathlib import Path
from typing import Any

from sleepyrouter.types import SleepyRouterModel

from .base import (
    BaseProviderAdapter,
    first_env,
    inject_max_reasoning,
)

GEMINI_OPENAI_BASE_URL = "https://generativelanguage.googleapis.com/v1beta/openai/"


class GoogleProviderAdapter(BaseProviderAdapter):
    """Google Gemini provider adapter utilizing Google's official OpenAI-compatible endpoint."""

    def __init__(self) -> None:
        super().__init__(
            name="Google",
            source="google",
            api_key_env_var="GOOGLE_API_KEY",
            message_protocol="openai",
            default_reasoning_effort="high",
            api_base=GEMINI_OPENAI_BASE_URL,
        )

    def get_api_key(self, env: dict[str, str] | None = None, root: Path | None = None) -> str:
        return first_env(["GOOGLE_API_KEY", "GEMINI_API_KEY"], env, root)

    def prepare_payload(
        self, model: SleepyRouterModel, request_kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = (model.upstream_id or model.id).removeprefix("gemini/")
        payload = inject_max_reasoning(
            request_kwargs,
            effort="high",
        )
        payload["model"] = upstream_id
        return payload

    def map_litellm_kwargs(
        self, model: SleepyRouterModel, api_key: str, kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = model.upstream_id or model.id
        res = inject_max_reasoning(
            kwargs,
            effort="high",
        )
        res.pop("thinking", None)
        res["model"] = f"gemini/{upstream_id}"
        res["api_key"] = api_key
        res["api_base"] = GEMINI_OPENAI_BASE_URL
        return res
