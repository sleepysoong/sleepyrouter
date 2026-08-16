"""API Key resolution and validation."""

import json
import os
from pathlib import Path

from sleepyrouter.types import ModelSource, ProviderAPIKeys
from sleepyrouter.utils import get_config_root, get_env_path, read_local_env


def _resolve_api_key(name: str, env: dict[str, str], local_env: dict[str, str]) -> str:
    env_val = (env.get(name) or "").strip()
    if env_val:
        return env_val
    return (local_env.get(name) or "").strip()


def _resolve_freebuff_key(
    env: dict[str, str], local_env: dict[str, str], root: Path | None = None
) -> str:
    key = _resolve_api_key("FREEBUFF_API_KEY", env, local_env) or _resolve_api_key(
        "CODEBUFF_API_KEY", env, local_env
    )
    if key:
        return key

    # Check credentials file under root or default ~/.config/manicode/credentials.json
    creds_candidates = []
    if root is not None:
        creds_candidates.append(root / "credentials.json")
    else:
        creds_candidates.append(Path.home() / ".config" / "manicode" / "credentials.json")

    for credentials_path in creds_candidates:
        if credentials_path.exists():
            try:
                data = json.loads(credentials_path.read_text(encoding="utf-8"))
                if isinstance(data, dict):
                    default_profile = data.get("default")
                    if isinstance(default_profile, dict):
                        token = default_profile.get("authToken") or default_profile.get("token")
                        if isinstance(token, str) and token.strip():
                            return token.strip()
            except (OSError, json.JSONDecodeError):
                pass
    return ""


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
        antigravity=_resolve_api_key("ANTIGRAVITY_API_KEY", env, local_env)
        or _resolve_api_key("GOOGLE_ANTIGRAVITY_TOKEN", env, local_env),
        freebuff=_resolve_freebuff_key(env, local_env, root),
    )


def api_key_for(keys: ProviderAPIKeys, source: ModelSource) -> str:
    switch_map = {
        "openrouter": keys.open_router,
        "nvidia": keys.nvidia,
        "copilot": keys.copilot,
        "zen": keys.zen,
        "google": keys.google,
        "antigravity": keys.antigravity
        or _resolve_api_key("ANTIGRAVITY_API_KEY", dict(os.environ), {})
        or _resolve_api_key("GOOGLE_ANTIGRAVITY_TOKEN", dict(os.environ), {}),
        "freebuff": keys.freebuff or _resolve_freebuff_key(dict(os.environ), {}, None),
    }
    if source in switch_map:
        return switch_map[source]
    custom_env_name = f"{source.upper().replace('-', '_')}_API_KEY"
    return os.environ.get(custom_env_name, "").strip()


def require_any_provider_api_key(
    env: dict[str, str] | None = None, root: Path | None = None
) -> ProviderAPIKeys:
    keys = resolve_provider_api_keys(env, root)
    if (
        not keys.open_router
        and not keys.nvidia
        and not keys.copilot
        and not keys.zen
        and not keys.google
        and not keys.antigravity
        and not keys.freebuff
    ):
        if root is None:
            root = get_config_root(env)
        env_file_path = get_env_path(root)
        err_msg = (
            "API 키가 설정되지 않았어요.\n"
            "  NVIDIA_API_KEY, OPENROUTER_API_KEY, GITHUB_COPILOT_TOKEN, "
            "OPENCODE_API_KEY, GOOGLE_API_KEY, ANTIGRAVITY_API_KEY, 또는 FREEBUFF_API_KEY 중 "
            "하나 이상이 필요해요.\n"
            "  설정 방법:\n"
            "    1. 환경변수: export GOOGLE_API_KEY=AIza...\n"
            f'    2. .env 파일: echo "GOOGLE_API_KEY=AIza..." > {env_file_path}'
        )
        raise ValueError(err_msg)
    return keys
