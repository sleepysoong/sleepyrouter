"""Antigravity OAuth compatibility shim (re-exported from antigravity.oauth)."""

from .antigravity.oauth import (
    ANTIGRAVITY_CLIENT_ID,
    ANTIGRAVITY_CLIENT_SECRET,
    GOOGLE_OAUTH_TOKEN_URL,
    async_refresh_antigravity_token,
    async_safe_force_refresh_token,
    force_refresh_antigravity_token,
    refresh_antigravity_token,
    resolve_antigravity_api_key,
    resolve_antigravity_project_id,
)

__all__ = [
    "ANTIGRAVITY_CLIENT_ID",
    "ANTIGRAVITY_CLIENT_SECRET",
    "GOOGLE_OAUTH_TOKEN_URL",
    "async_refresh_antigravity_token",
    "async_safe_force_refresh_token",
    "force_refresh_antigravity_token",
    "refresh_antigravity_token",
    "resolve_antigravity_api_key",
    "resolve_antigravity_project_id",
]
