"""Utility helpers for sleepyrouter."""

import os
from pathlib import Path
import re

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


def strip_html_tags(text: str) -> str:
    """Strip HTML tags and normalize whitespace if text contains HTML markup."""
    if "<" in text and ">" in text:
        clean = re.sub(r"<[^>]+>", " ", text)
        clean = re.sub(r"\s+", " ", clean).strip()
        return clean or text
    return text


def truncate(s: str, max_len: int) -> str:
    cleaned = strip_html_tags(s)
    return cleaned[:max_len] if len(cleaned) > max_len else cleaned


def safe_log_value(value: str) -> str:
    cleaned = strip_html_tags(value)
    sanitized = "".join(ch if ord(ch) >= 0x20 and ord(ch) != 0x7F else "?" for ch in cleaned)
    return (sanitized[:197] + "...") if len(sanitized) > 200 else sanitized
