"""JSON configuration store and SQLite usage logger."""

import json
from pathlib import Path
import sqlite3
import threading
from typing import Any

from sleepyrouter.types import ModelDefinition, SleepyRouterConfig, UsageLogEntry
from sleepyrouter.utils import get_config_path, get_config_root

DEFAULT_PORT = 4567


class ConfigStore:
    """Manages config.json storage and usage.db SQLite logging."""

    def __init__(self, root: Path | None = None) -> None:
        self.root = root or get_config_root()
        self.config_path = get_config_path(self.root)
        self.db_path = self.root / "usage.db"
        self._conn: sqlite3.Connection | None = None
        self._lock = threading.Lock()
        self._cached_mtime: float = -1.0
        self._cached_config: SleepyRouterConfig | None = None

    def ensure_root(self) -> None:
        self.root.mkdir(parents=True, exist_ok=True)

    def _init_db(self) -> sqlite3.Connection:
        if self._conn is None:
            self._conn = sqlite3.connect(
                str(self.db_path),
                timeout=10.0,
                check_same_thread=False,
            )
            self._conn.execute("PRAGMA journal_mode=WAL")
            self._conn.execute("PRAGMA busy_timeout=5000")
            self._conn.execute("PRAGMA synchronous=NORMAL")
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

    def read_config(self) -> SleepyRouterConfig:
        if not self.config_path.exists():
            return SleepyRouterConfig(port=DEFAULT_PORT, model_groups={})
        try:
            mtime = self.config_path.stat().st_mtime
            if self._cached_config is not None and mtime == self._cached_mtime:
                return self._cached_config
            data = json.loads(self.config_path.read_text(encoding="utf-8"))
        except (json.JSONDecodeError, OSError):
            return self._cached_config or SleepyRouterConfig(port=DEFAULT_PORT, model_groups={})

        config = SleepyRouterConfig(port=DEFAULT_PORT, model_groups={})
        if isinstance(data.get("port"), int):
            config.port = data["port"]

        if "modelGroups" in data and isinstance(data["modelGroups"], dict):
            config.model_groups = {
                k: [str(v) for v in vals if isinstance(v, (str, int))]
                for k, vals in data["modelGroups"].items()
                if isinstance(vals, list)
            }
            config.group_order = list(data["modelGroups"].keys())

        config.default_model_group = (
            data.get("defaultModelGroup") or data.get("defaultGroup") or None
        )

        if "models" in data and isinstance(data["models"], dict):
            models_map: dict[str, ModelDefinition] = {}
            for key, def_raw in data["models"].items():
                if isinstance(def_raw, dict):
                    max_effort = (
                        def_raw.get("maxEffort")
                        or def_raw.get("max_effort")
                        or def_raw.get("reasoningEffort")
                        or def_raw.get("reasoning_effort")
                    )
                    budget = def_raw.get("thinkingBudget") or def_raw.get("thinking_budget")
                    if isinstance(budget, str) and budget.isdigit():
                        budget = int(budget)

                    models_map[key] = ModelDefinition(
                        provider=def_raw.get("provider", ""),
                        name=def_raw.get("name", ""),
                        input_price=def_raw.get("inputPrice"),
                        output_price=def_raw.get("outputPrice"),
                        api_base=def_raw.get("apiBase") or def_raw.get("api_base"),
                        max_effort=str(max_effort) if max_effort is not None else None,
                        reasoning_effort=str(max_effort) if max_effort is not None else None,
                        thinking_budget=budget if isinstance(budget, int) else None,
                    )
            config.models = models_map

        self._cached_mtime = mtime
        self._cached_config = config
        return config

    def write_config(self, config: SleepyRouterConfig) -> None:
        self.ensure_root()
        data: dict[str, Any] = {
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
                    **({"inputPrice": v.input_price} if v.input_price is not None else {}),
                    **({"outputPrice": v.output_price} if v.output_price is not None else {}),
                    **({"apiBase": v.api_base} if v.api_base is not None else {}),
                    **(
                        {"maxEffort": v.max_effort or v.reasoning_effort}
                        if (v.max_effort or v.reasoning_effort) is not None
                        else {}
                    ),
                    **(
                        {"thinkingBudget": v.thinking_budget}
                        if v.thinking_budget is not None
                        else {}
                    ),
                }
                for k, v in config.models.items()
            }
        tmp = self.config_path.with_suffix(".tmp")
        tmp.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
        tmp.replace(self.config_path)
        try:
            self._cached_mtime = self.config_path.stat().st_mtime
            self._cached_config = config
        except OSError:
            self._cached_mtime = -1.0
            self._cached_config = None

    def append_usage(self, entry: UsageLogEntry) -> None:
        with self._lock:
            try:
                conn = self._init_db()
                with conn:
                    conn.execute(
                        """INSERT INTO usage_log
                           (ts, model, input_tokens, output_tokens, success)
                           VALUES (?, ?, ?, ?, ?)""",
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
            return [
                UsageLogEntry(
                    ts=r[0],
                    model=r[1],
                    input_tokens=r[2],
                    output_tokens=r[3],
                    success=bool(r[4]),
                )
                for r in cursor.fetchall()
            ]
        except sqlite3.Error:
            return []

    def get_initial_request_id(self) -> int:
        return self.get_request_count()

    def get_request_count(self) -> int:
        try:
            conn = self._init_db()
            cursor = conn.execute("SELECT COUNT(*) FROM usage_log")
            row = cursor.fetchone()
            return int(row[0]) if row and row[0] is not None else 0
        except sqlite3.Error:
            return 0

    def close(self) -> None:
        if self._conn:
            self._conn.close()
            self._conn = None


UsageLogger = ConfigStore
