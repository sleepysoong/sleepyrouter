"""Google Antigravity provider adapter package."""

from .adapter import AntigravityProviderAdapter
from .client import (
    AntigravityAPIError,
    AntigravityClientManager,
    call_antigravity_completion,
    call_antigravity_stream,
    get_antigravity_client,
)
from .oauth import (
    async_refresh_antigravity_token,
    async_safe_force_refresh_token,
    force_refresh_antigravity_token,
    refresh_antigravity_token,
    resolve_antigravity_api_key,
    resolve_antigravity_project_id,
)
from .serializer import (
    ANTIGRAVITY_NO_PREAMBLE_INSTRUCTION,
    ANTIGRAVITY_SYSTEM_INSTRUCTION,
    build_antigravity_payload,
    get_runtime_model_and_thinking_config,
    parse_antigravity_response,
    parse_antigravity_sse_chunk,
)

__all__ = [
    "ANTIGRAVITY_NO_PREAMBLE_INSTRUCTION",
    "ANTIGRAVITY_SYSTEM_INSTRUCTION",
    "AntigravityAPIError",
    "AntigravityClientManager",
    "AntigravityProviderAdapter",
    "async_refresh_antigravity_token",
    "async_safe_force_refresh_token",
    "build_antigravity_payload",
    "call_antigravity_completion",
    "call_antigravity_stream",
    "force_refresh_antigravity_token",
    "get_antigravity_client",
    "get_runtime_model_and_thinking_config",
    "parse_antigravity_response",
    "parse_antigravity_sse_chunk",
    "refresh_antigravity_token",
    "resolve_antigravity_api_key",
    "resolve_antigravity_project_id",
]
