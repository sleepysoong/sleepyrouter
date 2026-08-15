"""Config store and SQLite usage logger."""

import json
import os
import sqlite3
from pathlib import Path

from .routing import normalize_model_groups_ordered
from .types import (
    ModelDefinition,
    ProviderAPIKeys,
    SleepyRouterConfig,
    UsageLogEntry,
)
from .utils import (
    get_config_path,
    get_config_root,
    get_env_path,
    get_usage_path,
    read_local_env,
)

DEFAULT_PORT = 4567


class UsageLogger:
    def __init__(self, root: Path):
        self.db_path = root / "usage.db"
        self._conn: sqlite3.Connection | None = None

    def _init_db(self) -> sqlite3.Connection:
        if self._conn is None:
            self._conn = sqlite3.connect(str(self.db_path), check_same_thread=False)
            with self._conn:
                self._conn.execute(
                    """CREATE TABLE IF NOT EXISTS usage_log (
                        ts TEXT NOT NULL,
                        model TEXT NOT NULL,
                        input_tokens INTEGER NOT NULL,
                        output_tokens INTEGER NOT NULL,
                        success INTEGER NOT NULL
                    )"""
                )
        return self._conn

    def append_usage(self, entry: UsageLogEntry) -> None:
        try:
            conn = self._init_db()
            with conn:
                conn.execute(
                    "INSERT INTO usage_log (ts, model, input_tokens, output_tokens, success) VALUES (?, ?, ?, ?, ?)",
                    (
                        entry.ts,
                        entry.model,
                        entry.input_tokens,
                        entry.output_tokens,
                        1 if entry.success else 0,
                    ),
                )
        except sqlite3.Error:
            pass

    def read_usage_logs(self) -> list[UsageLogEntry]:
        try:
            conn = self._init_db()
            cursor = conn.execute(
                "SELECT ts, model, input_tokens, output_tokens, success FROM usage_log ORDER BY ts"
            )
            rows = cursor.fetchall()
            return [
                UsageLogEntry(
                    ts=r[0],
                    model=r[1],
                    input_tokens=r[2],
                    output_tokens=r[3],
                    success=bool(r[4]),
                )
                for r in rows
            ]
        except sqlite3.Error:
            return []

    def close(self) -> None:
        if self._conn:
            self._conn.close()
            self._conn = None


class ConfigStore:
    def __init__(self, root: Path | None = None):
        self.root = root or get_config_root()
        self.config_path = get_config_path(self.root)
        self.usage_path = get_usage_path(self.root)
        self.logger = UsageLogger(self.root)

    def ensure_root(self) -> None:
        self.root.mkdir(parents=True, exist_ok=True)

    def read_config(self) -> SleepyRouterConfig:
        if not self.config_path.exists():
            return SleepyRouterConfig(port=DEFAULT_PORT, model_groups={})
        try:
            data = json.loads(self.config_path.read_text(encoding="utf-8"))
        except (json.JSONDecodeError, OSError):
            return SleepyRouterConfig(port=DEFAULT_PORT, model_groups={})

        config = SleepyRouterConfig(port=DEFAULT_PORT, model_groups={})

        if isinstance(data.get("port"), int):
            config.port = data["port"]

        if "modelGroups" in data:
            groups, order = normalize_model_groups_ordered(data["modelGroups"])
            config.model_groups = groups
            if isinstance(data["modelGroups"], dict):
                config.group_order = list(data["modelGroups"].keys())
            else:
                config.group_order = order

        config.default_model_group = (
            data.get("defaultModelGroup") or data.get("defaultGroup") or None
        )

        if "models" in data and isinstance(data["models"], dict):
            models_map: dict[str, ModelDefinition] = {}
            for key, def_raw in data["models"].items():
                if isinstance(def_raw, dict):
                    models_map[key] = ModelDefinition(
                        provider=def_raw.get("provider", ""),
                        name=def_raw.get("name", ""),
                        input_price=def_raw.get("inputPrice"),
                        output_price=def_raw.get("outputPrice"),
                    )
            config.models = models_map

        return config

    def write_config(self, config: SleepyRouterConfig) -> None:
        self.ensure_root()
        data = {
            "port": config.port,
            "modelGroups": config.model_groups,
        }
        if config.default_model_group:
            data["defaultModelGroup"] = config.default_model_group
        if config.models:
            data["models"] = {
                k: {
                    "provider": v.provider,
                    "name": v.name,
                    **(
                        {"inputPrice": v.input_price}
                        if v.input_price is not None
                        else {}
                    ),
                    **(
                        {"outputPrice": v.output_price}
                        if v.output_price is not None
                        else {}
                    ),
                }
                for k, v in config.models.items()
            }
        tmp = self.config_path.with_suffix(".tmp")
        tmp.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
        tmp.replace(self.config_path)

    def append_usage(self, entry: UsageLogEntry) -> None:
        self.logger.append_usage(entry)

    def read_usage_logs(self) -> list[UsageLogEntry]:
        return self.logger.read_usage_logs()

    def close(self) -> None:
        self.logger.close()


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
    ):
        if root is None:
            root = get_config_root(env)
        raise ValueError(
            "API 키가 설정되지 않았어요.\n"
            "  NVIDIA_API_KEY, OPENROUTER_API_KEY, GITHUB_COPILOT_TOKEN, OPENCODE_API_KEY, 또는 GOOGLE_API_KEY 중 하나 이상이 필요해요.\n"
            f"  설정 방법:\n"
            "    1. 환경변수: export GOOGLE_API_KEY=AIza...\n"
            f'    2. .env 파일: echo "GOOGLE_API_KEY=AIza..." > {get_env_path(root)}'
        )
    return keys
