"""Utility helpers for sleepyrouter."""

import os
from pathlib import Path

CONFIG_FILE_NAME = "config.json"
USAGE_FILE_NAME = "usage.jsonl"


def get_config_root(env: dict[str, str] | None = None) -> Path:
    if env is None:
        env = dict(os.environ)
    if env.get("SLEEPYROUTER_HOME"):
        return Path(env["SLEEPYROUTER_HOME"])
    return Path.home() / ".sleepyrouter"


def get_config_path(root: Path) -> Path:
    return root / CONFIG_FILE_NAME


def get_usage_path(root: Path) -> Path:
    return root / USAGE_FILE_NAME


def get_env_path(root: Path) -> Path:
    return root / ".env"


def parse_dotenv(content: str) -> dict[str, str]:
    values: dict[str, str] = {}
    for raw_line in content.replace("\r\n", "\n").split("\n"):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip()
        if not key:
            continue
        if len(value) >= 2 and (
            (value[0] == '"' and value[-1] == '"') or (value[0] == "'" and value[-1] == "'")
        ):
            value = value[1:-1]
        values[key] = value
    return values


def read_local_env(root: Path) -> dict[str, str]:
    env_path = get_env_path(root)
    if not env_path.exists():
        return {}
    try:
        return parse_dotenv(env_path.read_text(encoding="utf-8"))
    except OSError:
        return {}


def truncate(s: str, max_len: int) -> str:
    return s[:max_len] if len(s) > max_len else s


def safe_log_value(value: str) -> str:
    sanitized = "".join(ch if ord(ch) >= 0x20 and ord(ch) != 0x7F else "?" for ch in value)
    return (sanitized[:197] + "...") if len(sanitized) > 200 else sanitized
