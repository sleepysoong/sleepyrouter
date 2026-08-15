"""Base ProviderAdapter and ProviderRegistry abstraction."""

import os
from typing import Any, Protocol

from sleepyrouter.types import ModelSource, SleepyRouterModel, source_of

MessageProtocol = str  # "openai" | "anthropic"


def base_url_from(env_var: str, def_url: str) -> str:
    return os.environ.get(env_var, def_url)


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
        message_protocol: MessageProtocol = "openai",
    ):
        self._name = name
        self._source = source
        self._api_key_env_var = api_key_env_var
        self._message_protocol = message_protocol

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
        litellm_kwargs = dict(kwargs)
        litellm_kwargs["model"] = f"openai/{upstream_id}"
        litellm_kwargs["api_key"] = api_key
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
        return adapter.map_litellm_kwargs(model, prepared_key, kwargs)

    upstream_id = model.upstream_id or model.id
    res = dict(kwargs)
    res["model"] = f"openai/{upstream_id}"
    res["api_key"] = api_key
    return res
