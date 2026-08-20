"""Freebuff (Codebuff) provider adapter."""

from .base import BaseProviderAdapter

FREEBUFF_BASE_URL = "https://codebuff.com/api/v1"


class FreebuffProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="Freebuff",
            source="freebuff",
            api_key_env_var="FREEBUFF_API_KEY",
            api_base=FREEBUFF_BASE_URL,
            extra_headers={"User-Agent": "freebuff/1.0.0"},
            default_reasoning_effort="high",
        )
