"""Antigravity Provider Adapter implementation."""

from collections.abc import AsyncGenerator
import json
from pathlib import Path
import sys
from typing import Any

from sleepyrouter.providers.base import BaseProviderAdapter, inject_max_reasoning
from sleepyrouter.types import SleepyRouterModel

from .client import (
    ANTIGRAVITY_BASE_URL,
    ANTIGRAVITY_CLIENT_HEADER,
    ANTIGRAVITY_USER_AGENT,
)
from .oauth import resolve_antigravity_api_key
from .serializer import get_runtime_model_and_thinking_config


class AntigravityProviderAdapter(BaseProviderAdapter):
    """Adapter for Google Antigravity (CloudCode) internal endpoints."""

    def __init__(self) -> None:
        super().__init__(
            name="Google Antigravity",
            source="antigravity",
            api_key_env_var="ANTIGRAVITY_API_KEY",
            message_protocol="openai",
            default_reasoning_effort="high",
            default_thinking_budget=32000,
        )

    def get_api_key(
        self, env: dict[str, str] | None = None, root: Path | None = None
    ) -> str:
        return resolve_antigravity_api_key(env, root)

    def map_litellm_kwargs(
        self, model: SleepyRouterModel, api_key: str, kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = model.upstream_id or model.id
        runtime_id, _ = get_runtime_model_and_thinking_config(upstream_id)
        res = inject_max_reasoning(
            kwargs,
            effort="high",
            thinking_budget=32000,
        )
        res["model"] = f"openai/{runtime_id}"
        res["api_base"] = ANTIGRAVITY_BASE_URL
        res["api_key"] = api_key
        res["headers"] = {
            "User-Agent": ANTIGRAVITY_USER_AGENT,
            "X-Goog-Api-Client": ANTIGRAVITY_CLIENT_HEADER,
            "Client-Metadata": json.dumps(
                {
                    "ideType": "ANTIGRAVITY",
                    "platform": "PLATFORM_UNSPECIFIED",
                    "pluginType": "GEMINI",
                }
            ),
        }
        return res

    async def complete(
        self,
        model: SleepyRouterModel,
        api_key: str,
        request_kwargs: dict[str, Any],
        timeout: float = 60.0,
    ) -> dict[str, Any]:
        if model.api_base:
            return await super().complete(model, api_key, request_kwargs, timeout=timeout)
        upstream_id = model.upstream_id or model.id
        pkg = sys.modules.get("sleepyrouter.providers.antigravity")
        fn = getattr(pkg, "call_antigravity_completion", None)
        if fn is None:
            from .client import call_antigravity_completion

            fn = call_antigravity_completion
        return await fn(upstream_id, api_key, request_kwargs, timeout=timeout)

    async def stream(
        self,
        model: SleepyRouterModel,
        api_key: str,
        request_kwargs: dict[str, Any],
        timeout: float = 60.0,
    ) -> AsyncGenerator[Any, None]:
        if model.api_base:
            return await super().stream(model, api_key, request_kwargs, timeout=timeout)
        upstream_id = model.upstream_id or model.id
        pkg = sys.modules.get("sleepyrouter.providers.antigravity")
        fn = getattr(pkg, "call_antigravity_stream", None)
        if fn is None:
            from .client import call_antigravity_stream

            fn = call_antigravity_stream
        return fn(upstream_id, api_key, request_kwargs, timeout=timeout)
