"""JSON configuration store management."""

import json
from pathlib import Path
from typing import Any

from sleepyrouter.types import ModelDefinition, SleepyRouterConfig, UsageLogEntry
from sleepyrouter.utils import (
    get_config_path,
    get_config_root,
    get_usage_path,
)

from .logger import UsageLogger

DEFAULT_PORT = 4567


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

        if "modelGroups" in data and isinstance(data["modelGroups"], dict):
            groups = {
                k: [str(v) for v in vals if isinstance(v, (str, int))]
                for k, vals in data["modelGroups"].items()
                if isinstance(vals, list)
            }
            config.model_groups = groups
            config.group_order = list(data["modelGroups"].keys())

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
