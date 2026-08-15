"""API Key resolution and validation."""

import os
from pathlib import Path

from sleepyrouter.types import ModelSource, ProviderAPIKeys
from sleepyrouter.utils import get_config_root, get_env_path, read_local_env


def _resolve_api_key(name: str, env: dict[str, str], local_env: dict[str, str]) -> str:
    env_val = (env.get(name) or "").strip()
    if env_val:
        return env_val
    return (local_env.get(name) or "").strip()


def resolve_provider_api_keys(
    env: dict[str, str] | None = None, root: Path | None = None
) -> ProviderAPIKeys:
    if env is None:
        env = dict(os.environ)
    if root is None:
        root = get_config_root(env)
    local_env = read_local_env(root)

    return ProviderAPIKeys(
        open_router=_resolve_api_key("OPENROUTER_API_KEY", env, local_env),
        nvidia=_resolve_api_key("NVIDIA_API_KEY", env, local_env),
        copilot=_resolve_api_key("GITHUB_COPILOT_TOKEN", env, local_env),
        zen=_resolve_api_key("OPENCODE_API_KEY", env, local_env),
        google=_resolve_api_key("GOOGLE_API_KEY", env, local_env)
        or _resolve_api_key("GEMINI_API_KEY", env, local_env),
    )


def api_key_for(keys: ProviderAPIKeys, source: ModelSource) -> str:
    switch_map = {
        "openrouter": keys.open_router,
        "nvidia": keys.nvidia,
        "copilot": keys.copilot,
        "zen": keys.zen,
        "google": keys.google,
        "antigravity": _resolve_api_key("ANTIGRAVITY_API_KEY", dict(os.environ), {})
        or _resolve_api_key("GOOGLE_ANTIGRAVITY_TOKEN", dict(os.environ), {}),
    }
    if source in switch_map:
        return switch_map[source]
    custom_env_name = f"{source.upper().replace('-', '_')}_API_KEY"
    return os.environ.get(custom_env_name, "").strip()


def require_any_provider_api_key(
    env: dict[str, str] | None = None, root: Path | None = None
) -> ProviderAPIKeys:
    keys = resolve_provider_api_keys(env, root)
    has_antigravity = bool(
        os.environ.get("ANTIGRAVITY_API_KEY")
        or os.environ.get("GOOGLE_ANTIGRAVITY_TOKEN")
    )
    if (
        not keys.open_router
        and not keys.nvidia
        and not keys.copilot
        and not keys.zen
        and not keys.google
        and not has_antigravity
    ):
        if root is None:
            root = get_config_root(env)
        raise ValueError(
            "API 키가 설정되지 않았어요.\n"
            "  NVIDIA_API_KEY, OPENROUTER_API_KEY, GITHUB_COPILOT_TOKEN, OPENCODE_API_KEY, GOOGLE_API_KEY, 또는 ANTIGRAVITY_API_KEY 중 하나 이상이 필요해요.\n"
            "  설정 방법:\n"
            "    1. 환경변수: export GOOGLE_API_KEY=AIza...\n"
            f'    2. .env 파일: echo "GOOGLE_API_KEY=AIza..." > {get_env_path(root)}'
        )
    return keys
