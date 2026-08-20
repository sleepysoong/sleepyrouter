"""Base ProviderAdapter and ProviderRegistry abstraction with reasoning & thinking injection."""

from typing import Any, Protocol

from sleepyrouter.types import ModelSource, SleepyRouterModel, source_of

MessageProtocol = str  # "openai"


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
