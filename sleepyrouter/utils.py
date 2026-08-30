"""Utility helpers for sleepyrouter."""

import io
import os
from pathlib import Path
import re

from dotenv import dotenv_values

CONFIG_FILE_NAME = "config.json"
USAGE_FILE_NAME = "usage.jsonl"


def get_config_root(env: dict[str, str] | None = None) -> Path:
    resolved_env = dict(os.environ) if env is None else env
    if resolved_env.get("SLEEPYROUTER_HOME"):
        return Path(resolved_env["SLEEPYROUTER_HOME"])
    return Path.home() / ".sleepyrouter"


def get_config_path(root: Path) -> Path:
    return root / CONFIG_FILE_NAME


def get_usage_path(root: Path) -> Path:
    return root / USAGE_FILE_NAME


def get_env_path(root: Path) -> Path:
    return root / ".env"


def parse_dotenv(content: str) -> dict[str, str]:
    parsed = dotenv_values(stream=io.StringIO(content))
    return {k: str(v) for k, v in parsed.items() if v is not None}


def read_local_env(root: Path) -> dict[str, str]:
    env_path = get_env_path(root)
    if not env_path.exists():
        return {}
    try:
        return parse_dotenv(env_path.read_text(encoding="utf-8"))
    except OSError:
        return {}


def safe_exists(path: Path) -> bool:
    try:
        return path.exists()
    except OSError:
        return False


def strip_html_tags(text: str) -> str:
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


def format_error_message(exc: Exception) -> str:
    msg = str(exc).strip()
    exc_type = type(exc).__name__
    if msg:
        return f"{exc_type}: {msg}" if not msg.startswith(exc_type) else msg
    return exc_type
