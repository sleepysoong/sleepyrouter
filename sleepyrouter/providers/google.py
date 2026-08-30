"""Google Gemini provider adapter using official OpenAI-compatible endpoint."""

from pathlib import Path

from .base import BaseProviderAdapter, first_env

GEMINI_OPENAI_BASE_URL = "https://generativelanguage.googleapis.com/v1beta/openai/"


class GoogleProviderAdapter(BaseProviderAdapter):
    """Google Gemini provider adapter utilizing Google's official OpenAI-compatible endpoint."""

    def __init__(self) -> None:
        super().__init__(
            name="Google",
            source="google",
            api_key_env_var="GOOGLE_API_KEY",
            message_protocol="openai",
            api_base=GEMINI_OPENAI_BASE_URL,
        )

    def get_api_key(self, env: dict[str, str] | None = None, root: Path | None = None) -> str:
        return first_env(["GOOGLE_API_KEY", "GEMINI_API_KEY"], env, root)
