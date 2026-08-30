"""Zen (OpenCode) provider adapter."""

from .base import BaseProviderAdapter


class ZenProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="Zen",
            source="zen",
            api_key_env_var="OPENCODE_API_KEY",
            api_base="https://opencode.ai/zen/v1",
        )
