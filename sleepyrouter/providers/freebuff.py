"""Freebuff (Codebuff) provider adapter."""

import json
from pathlib import Path

from .base import BaseProviderAdapter, first_env, safe_exists

FREEBUFF_BASE_URL = "https://codebuff.com/api/v1"
_CREDENTIALS_FILENAME = "credentials.json"


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

    def get_api_key(
        self, env: dict[str, str] | None = None, root: Path | None = None
    ) -> str:
        key = first_env(["FREEBUFF_API_KEY", "CODEBUFF_API_KEY"], env, root)
        if key:
            return key

        if root is not None:
            credentials_path = root / _CREDENTIALS_FILENAME
        else:
            credentials_path = Path.home() / ".config" / "manicode" / _CREDENTIALS_FILENAME
        if not safe_exists(credentials_path):
            return ""
        try:
            data = json.loads(credentials_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            return ""
        default_profile = data.get("default") if isinstance(data, dict) else None
        if not isinstance(default_profile, dict):
            return ""
        token = default_profile.get("authToken") or default_profile.get("token")
        return token.strip() if isinstance(token, str) else ""
