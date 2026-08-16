"""API Key resolution and validation."""

import base64
import datetime
import json
import logging
import os
from pathlib import Path
import time
from typing import Any
import urllib.parse
import urllib.request

from sleepyrouter.types import ModelSource, ProviderAPIKeys
from sleepyrouter.utils import get_config_root, get_env_path, read_local_env

logger = logging.getLogger("sleepyrouter.config")


def _get_default_antigravity_client_id() -> str:
    s1 = "MTA3MTAwNjA2MDU5MS10bWhzc2luMmgyMWxjcmUyMzV2dG9sb2poNGc0MDNlc"
    s2 = "C5hcHBzLmdvb2dsZXVzZXJjb250ZW50LmNvbQ=="
    return base64.b64decode(s1 + s2).decode("utf-8")


def _get_default_antigravity_client_secret() -> str:
    k1 = "R09DU1BYLUs1OEZXUjQ"
    k2 = "4NkxkTEoxbUxCOHNYQzR6NnFEQWY="
    return base64.b64decode(k1 + k2).decode("utf-8")


ANTIGRAVITY_CLIENT_ID = (
    os.environ.get("ANTIGRAVITY_CLIENT_ID", "").strip() or _get_default_antigravity_client_id()
)
ANTIGRAVITY_CLIENT_SECRET = (
    os.environ.get("ANTIGRAVITY_CLIENT_SECRET", "").strip()
    or _get_default_antigravity_client_secret()
)
GOOGLE_OAUTH_TOKEN_URL = "https://oauth2.googleapis.com/token"  # noqa: S105


def _resolve_api_key(name: str, env: dict[str, str], local_env: dict[str, str]) -> str:
    env_val = (env.get(name) or "").strip()
    if env_val:
        return env_val
    return (local_env.get(name) or "").strip()


def refresh_antigravity_token(refresh_token: str) -> tuple[str, int]:
    """Refreshes Antigravity OAuth access token via Google OAuth token endpoint."""
    data = urllib.parse.urlencode(
        {
            "client_id": ANTIGRAVITY_CLIENT_ID,
            "client_secret": ANTIGRAVITY_CLIENT_SECRET,
            "refresh_token": refresh_token,
            "grant_type": "refresh_token",
        }
    ).encode("utf-8")

    req = urllib.request.Request(
        GOOGLE_OAUTH_TOKEN_URL,
        data=data,
        headers={"Content-Type": "application/x-www-form-urlencoded"},
    )
    with urllib.request.urlopen(req, timeout=10) as resp:  # noqa: S310
        res_json = json.loads(resp.read().decode("utf-8"))
        access_token = str(res_json.get("access_token") or "")
        expires_in = int(res_json.get("expires_in") or 3600)
        return access_token, expires_in


def _get_antigravity_candidate_paths(root: Path | None = None) -> list[Path]:
    if root is not None:
        return [root / "auth.json", root / "antigravity-oauth-token"]
    return [
        Path.home() / ".senpi" / "agent" / "auth.json",
        Path.home() / ".gemini" / "antigravity-cli" / "antigravity-oauth-token",
        Path.home() / ".gemini" / "antigravity" / "antigravity-oauth-token",
    ]


def _process_auth_json(data: dict[str, Any], path: Path, *, force: bool) -> str:
    ag = data.get("antigravity")
    if not isinstance(ag, dict):
        return ""
    access = str(ag.get("access") or "").strip()
    refresh = str(ag.get("refresh") or "").strip()
    expires = ag.get("expires")
    now_ms = time.time() * 1000

    should_refresh = force or not access
    if not should_refresh and isinstance(expires, (int, float)) and now_ms >= expires - 300_000:
        should_refresh = True

    if should_refresh and refresh:
        try:
            new_acc, exp_sec = refresh_antigravity_token(refresh)
            if new_acc:
                ag["access"] = new_acc
                ag["expires"] = int((time.time() + exp_sec) * 1000)
                data["antigravity"] = ag
                path.write_text(json.dumps(data, indent=2), encoding="utf-8")
                return new_acc
        except Exception as exc:  # noqa: BLE001
            logger.debug("Failed refreshing Antigravity auth.json: %s", exc)
    return access


def _process_oauth_token_file(data: dict[str, Any], path: Path, *, force: bool) -> str:
    tok = data.get("token")
    if not isinstance(tok, dict):
        return ""
    access_token = str(tok.get("access_token") or "").strip()
    refresh_token = str(tok.get("refresh_token") or "").strip()
    expiry_str = str(tok.get("expiry") or "").strip()

    should_refresh = force or not access_token
    if not should_refresh and expiry_str:
        try:
            exp_dt = datetime.datetime.fromisoformat(expiry_str)
            now_dt = datetime.datetime.now(datetime.UTC)
            if now_dt >= exp_dt - datetime.timedelta(minutes=5):
                should_refresh = True
        except ValueError:
            pass

    if should_refresh and refresh_token:
        try:
            new_acc, exp_sec = refresh_antigravity_token(refresh_token)
            if new_acc:
                tok["access_token"] = new_acc
                new_exp_dt = datetime.datetime.now(datetime.UTC) + datetime.timedelta(
                    seconds=exp_sec
                )
                tok["expiry"] = new_exp_dt.isoformat()
                data["token"] = tok
                path.write_text(json.dumps(data, indent=2), encoding="utf-8")
                return new_acc
        except Exception as exc:  # noqa: BLE001
            logger.debug("Failed refreshing antigravity-oauth-token file: %s", exc)
    return access_token


def _extract_antigravity_token(path: Path, *, force: bool = False) -> str:
    if not path.exists():
        return ""
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
        if not isinstance(data, dict):
            return ""
        if "antigravity" in data:
            return _process_auth_json(data, path, force=force)
        if "token" in data:
            return _process_oauth_token_file(data, path, force=force)
    except (OSError, json.JSONDecodeError) as exc:
        logger.debug("Error reading token file %s: %s", path, exc)
    return ""


def force_refresh_antigravity_token(root: Path | None = None) -> str:
    """Forces refreshing the Antigravity OAuth token from local credential files."""
    for p in _get_antigravity_candidate_paths(root):
        refreshed = _extract_antigravity_token(p, force=True)
        if refreshed:
            return refreshed
    return ""


def _resolve_antigravity_key(
    env: dict[str, str], local_env: dict[str, str], root: Path | None = None
) -> str:
    key = _resolve_api_key("ANTIGRAVITY_API_KEY", env, local_env) or _resolve_api_key(
        "GOOGLE_ANTIGRAVITY_TOKEN", env, local_env
    )
    if key:
        return key

    for p in _get_antigravity_candidate_paths(root):
        tok = _extract_antigravity_token(p, force=False)
        if tok:
            return tok
    return ""


def _resolve_freebuff_key(
    env: dict[str, str], local_env: dict[str, str], root: Path | None = None
) -> str:
    key = _resolve_api_key("FREEBUFF_API_KEY", env, local_env) or _resolve_api_key(
        "CODEBUFF_API_KEY", env, local_env
    )
    if key:
        return key

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
        antigravity=_resolve_antigravity_key(env, local_env, root),
        freebuff=_resolve_freebuff_key(env, local_env, root),
    )


def api_key_for(keys: ProviderAPIKeys, source: ModelSource) -> str:
    switch_map = {
        "openrouter": keys.open_router,
        "nvidia": keys.nvidia,
        "copilot": keys.copilot,
        "zen": keys.zen,
        "google": keys.google,
        "antigravity": keys.antigravity or _resolve_antigravity_key(dict(os.environ), {}, None),
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
