"""Base ProviderAdapter and ProviderRegistry abstraction using official AsyncOpenAI SDK."""

from collections.abc import AsyncGenerator, Sequence
import os
from pathlib import Path
from typing import Any, Protocol

from openai import AsyncOpenAI

from sleepyrouter.types import ModelSource, SleepyRouterModel, source_of
from sleepyrouter.utils import get_config_root, read_local_env

MessageProtocol = str  # "openai"


def first_env(
    names: Sequence[str], env: dict[str, str] | None = None, root: Path | None = None
) -> str:
    """Resolves the first non-empty env var: process env wins over local .env file."""
    resolved_env = dict(os.environ) if env is None else env
    config_root = root if root is not None else get_config_root(resolved_env)
    local_env = read_local_env(config_root)
    for name in names:
        key = (resolved_env.get(name) or "").strip() or (local_env.get(name) or "").strip()
        if key:
            return key
    return ""


def safe_exists(path: Path) -> bool:
    """Path.exists() re-raises EACCES on unreadable parents; treat those as missing."""
    try:
        return path.exists()
    except OSError:
        return False


def inject_max_reasoning(
    kwargs: dict[str, Any],
    effort: str = "high",
    thinking_budget: int | None = None,
) -> dict[str, Any]:
    res = dict(kwargs)
    if "reasoning_effort" not in res:
        res["reasoning_effort"] = effort
    if thinking_budget is not None and "thinking" not in res:
        res["thinking"] = {"type": "enabled", "budget_tokens": thinking_budget}
    return res


class ProviderAdapter(Protocol):
    @property
    def name(self) -> str: ...

    @property
    def source(self) -> ModelSource: ...

    @property
    def api_key_env_var(self) -> str: ...

    @property
    def message_protocol(self) -> MessageProtocol: ...

    def prepare_api_key(self, api_key: str) -> str: ...

    def map_litellm_kwargs(
        self, model: SleepyRouterModel, api_key: str, kwargs: dict[str, Any]
    ) -> dict[str, Any]: ...

    async def complete(
        self,
        model: SleepyRouterModel,
        api_key: str,
        request_kwargs: dict[str, Any],
        timeout: float = 60.0,
    ) -> dict[str, Any]: ...

    async def stream(
        self,
        model: SleepyRouterModel,
        api_key: str,
        request_kwargs: dict[str, Any],
        timeout: float = 60.0,
    ) -> AsyncGenerator[Any, None]: ...


class BaseProviderAdapter:
    def __init__(
        self,
        name: str,
        source: ModelSource,
        api_key_env_var: str,
        *,
        message_protocol: MessageProtocol = "openai",
        default_reasoning_effort: str = "high",
        default_thinking_budget: int | None = None,
        api_base: str | None = None,
        model_prefix: str = "openai",
        extra_headers: dict[str, str] | None = None,
    ) -> None:
        self._name = name
        self._source = source
        self._api_key_env_var = api_key_env_var
        self._message_protocol = message_protocol
        self._default_reasoning_effort = default_reasoning_effort
        self._default_thinking_budget = default_thinking_budget
        self._api_base = api_base
        self._model_prefix = model_prefix
        self._extra_headers = extra_headers or {}

    @property
    def name(self) -> str:
        return self._name

    @property
    def source(self) -> ModelSource:
        return self._source

    @property
    def api_key_env_var(self) -> str:
        return self._api_key_env_var

    @property
    def message_protocol(self) -> MessageProtocol:
        return self._message_protocol

    def prepare_api_key(self, api_key: str) -> str:
        return api_key

    def get_api_key(self, env: dict[str, str] | None = None, root: Path | None = None) -> str:
        return first_env([self._api_key_env_var], env, root)

    def get_client(
        self, api_key: str, api_base: str | None = None, timeout: float = 60.0
    ) -> AsyncOpenAI:
        base_url = api_base or self._api_base
        return AsyncOpenAI(
            api_key=api_key,
            base_url=base_url,
            default_headers=self._extra_headers or None,
            timeout=timeout,
            max_retries=0,
        )

    def prepare_payload(
        self, model: SleepyRouterModel, request_kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = model.upstream_id or model.id
        if "/" in upstream_id and upstream_id.startswith(("openai/", "gemini/", "openrouter/")):
            upstream_id = upstream_id.split("/", 1)[1]
        effort = (
            request_kwargs.get("reasoning_effort")
            or model.reasoning_effort
            or model.max_effort
            or self._default_reasoning_effort
        )
        budget = (
            request_kwargs.get("thinking_budget")
            or model.thinking_budget
            or self._default_thinking_budget
        )
        payload = inject_max_reasoning(
            request_kwargs,
            effort=effort,
            thinking_budget=budget,
        )
        payload["model"] = upstream_id
        return payload

    def map_litellm_kwargs(
        self, model: SleepyRouterModel, api_key: str, kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = model.upstream_id or model.id
        litellm_kwargs = inject_max_reasoning(
            kwargs,
            effort=self._default_reasoning_effort,
            thinking_budget=self._default_thinking_budget,
        )
        litellm_kwargs["model"] = f"{self._model_prefix}/{upstream_id}"
        litellm_kwargs["api_key"] = api_key
        if self._api_base:
            litellm_kwargs["api_base"] = self._api_base
        if self._extra_headers:
            litellm_kwargs["headers"] = dict(self._extra_headers)
        return litellm_kwargs

    async def complete(
        self,
        model: SleepyRouterModel,
        api_key: str,
        request_kwargs: dict[str, Any],
        timeout: float = 60.0,
    ) -> dict[str, Any]:
        prepared_key = self.prepare_api_key(api_key)
        client = self.get_client(prepared_key, api_base=model.api_base, timeout=timeout)
        payload = self.prepare_payload(model, request_kwargs)
        payload.pop("stream", None)
        resp_obj = await client.chat.completions.create(**payload)
        return resp_obj.model_dump() if hasattr(resp_obj, "model_dump") else dict(resp_obj)

    async def stream(
        self,
        model: SleepyRouterModel,
        api_key: str,
        request_kwargs: dict[str, Any],
        timeout: float = 60.0,
    ) -> AsyncGenerator[Any, None]:
        prepared_key = self.prepare_api_key(api_key)
        client = self.get_client(prepared_key, api_base=model.api_base, timeout=timeout)
        payload = self.prepare_payload(model, request_kwargs)
        payload.pop("stream", None)
        payload["stream_options"] = {"include_usage": True}
        return await client.chat.completions.create(**payload, stream=True)  # type: ignore[no-any-return]


class ProviderRegistry:
    def __init__(self) -> None:
        self.adapters: dict[str, BaseProviderAdapter] = {}

    def register(self, adapter: BaseProviderAdapter) -> "ProviderRegistry":
        self.adapters[adapter.source] = adapter
        return self

    def get(self, source: ModelSource) -> BaseProviderAdapter | None:
        return self.adapters.get(source)

    def has(self, source: ModelSource) -> bool:
        return source in self.adapters

    def get_all(self) -> list[BaseProviderAdapter]:
        return list(self.adapters.values())


default_provider_registry = ProviderRegistry()


def map_to_litellm_kwargs(
    model: SleepyRouterModel,
    api_key: str,
    kwargs: dict[str, Any],
) -> dict[str, Any]:
    source = source_of(model)
    adapter = default_provider_registry.get(source)
    if adapter:
        prepared_key = adapter.prepare_api_key(api_key)
        res = adapter.map_litellm_kwargs(model, prepared_key, kwargs)
        if model.api_base:
            res["api_base"] = model.api_base
        return res

    upstream_id = model.upstream_id or model.id
    res = inject_max_reasoning(kwargs, effort="high")
    res["model"] = f"openai/{upstream_id}"
    res["api_key"] = api_key
    if model.api_base:
        res["api_base"] = model.api_base
    return res
