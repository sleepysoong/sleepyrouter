from .antigravity import AntigravityProviderAdapter
from .base import (
    BaseProviderAdapter,
    MessageProtocol,
    ProviderAdapter,
    ProviderRegistry,
    default_provider_registry,
    inject_max_reasoning,
    map_to_litellm_kwargs,
)
from .copilot import CopilotProviderAdapter, exchange_copilot_token
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

__all__ = [
    "AntigravityProviderAdapter",
    "BaseProviderAdapter",
    "CopilotProviderAdapter",
    "GoogleProviderAdapter",
    "MessageProtocol",
    "NVIDIAProviderAdapter",
    "OpenRouterProviderAdapter",
    "ProviderAdapter",
    "ProviderRegistry",
    "ZenProviderAdapter",
    "default_provider_registry",
    "exchange_copilot_token",
    "inject_max_reasoning",
    "map_to_litellm_kwargs",
]
