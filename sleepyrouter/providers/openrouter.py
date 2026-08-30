"""OpenRouter provider adapter."""

from .base import BaseProviderAdapter


class OpenRouterProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="OpenRouter",
            source="openrouter",
            api_key_env_var="OPENROUTER_API_KEY",
            api_base="https://openrouter.ai/api/v1",
            model_prefix="openrouter",
            extra_headers={
                "HTTP-Referer": "https://github.com/sleepysoong/sleepyrouter",
                "X-OpenRouter-Title": "sleepyrouter",
            },
        )
