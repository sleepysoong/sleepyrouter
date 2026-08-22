"""Provider adapters, registry, and API key resolution entry points."""

import os
from pathlib import Path

from sleepyrouter.types import ModelSource

from .antigravity import AntigravityProviderAdapter
from .base import (
    BaseProviderAdapter,
    MessageProtocol,
    ProviderAdapter,
    ProviderRegistry,
    default_provider_registry,
    first_env,
    inject_max_reasoning,
    map_to_litellm_kwargs,
)
from .copilot import CopilotProviderAdapter, exchange_copilot_token
from .freebuff import FreebuffProviderAdapter
from .google import GoogleProviderAdapter
from .nvidia import NVIDIAProviderAdapter
from .openrouter import OpenRouterProviderAdapter
from .zen import ZenProviderAdapter

# Register default adapters
default_provider_registry.register(OpenRouterProviderAdapter())
default_provider_registry.register(NVIDIAProviderAdapter())
default_provider_registry.register(CopilotProviderAdapter())
default_provider_registry.register(GoogleProviderAdapter())
default_provider_registry.register(ZenProviderAdapter())
default_provider_registry.register(AntigravityProviderAdapter())
default_provider_registry.register(FreebuffProviderAdapter())


def api_key_for(source: ModelSource, env: dict[str, str] | None = None) -> str:
    """Resolves the API key for a provider source via its adapter.

    Unknown (custom) sources fall back to the ``{SOURCE}_API_KEY`` env var convention.
    """
    adapter = default_provider_registry.get(source)
    if adapter:
        return adapter.get_api_key(env)

    custom_env_name = f"{source.upper().replace('-', '_')}_API_KEY"
    resolved_env = dict(os.environ) if env is None else env
    return resolved_env.get(custom_env_name, "").strip()


def require_any_provider_api_key(
    env: dict[str, str] | None = None, root: Path | None = None
) -> bool:
    """Returns True when at least one configured provider has a usable key."""
    if any(adapter.get_api_key(env, root) for adapter in default_provider_registry.get_all()):
        return True

    env_names = ", ".join(
        adapter.api_key_env_var for adapter in default_provider_registry.get_all()
    )
    err_msg = (
        "API 키가 설정되지 않았어요.\n"
        f"  {env_names} 중 하나 이상이 필요해요.\n"
        "  설정 방법:\n"
        "    1. 환경변수: export GOOGLE_API_KEY=AIza...\n"
        '    2. .env 파일: echo "GOOGLE_API_KEY=AIza..." > ~/.sleepyrouter/.env'
    )
    raise ValueError(err_msg)


__all__ = [
    "AntigravityProviderAdapter",
    "BaseProviderAdapter",
    "CopilotProviderAdapter",
    "FreebuffProviderAdapter",
    "GoogleProviderAdapter",
    "MessageProtocol",
    "NVIDIAProviderAdapter",
    "OpenRouterProviderAdapter",
    "ProviderAdapter",
    "ProviderRegistry",
    "ZenProviderAdapter",
    "api_key_for",
    "default_provider_registry",
    "exchange_copilot_token",
    "first_env",
    "inject_max_reasoning",
    "map_to_litellm_kwargs",
    "require_any_provider_api_key",
]
