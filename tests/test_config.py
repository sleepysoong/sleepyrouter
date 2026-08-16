from pathlib import Path
import tempfile
from unittest.mock import patch

import pytest

from sleepyrouter.config import (
    ConfigStore,
    api_key_for,
    force_refresh_antigravity_token,
    require_any_provider_api_key,
    resolve_provider_api_keys,
)
from sleepyrouter.types import SleepyRouterConfig, UsageLogEntry
from sleepyrouter.utils import parse_dotenv, safe_log_value, strip_html_tags, truncate


def test_parse_dotenv() -> None:
    dotenv = """
    # Comment
    OPENROUTER_API_KEY="sk-or-test"
    NVIDIA_API_KEY='nvapi-test'
    PLAIN_KEY=plain_val
    """
    parsed = parse_dotenv(dotenv)
    assert parsed == {
        "OPENROUTER_API_KEY": "sk-or-test",
        "NVIDIA_API_KEY": "nvapi-test",
        "PLAIN_KEY": "plain_val",
    }


def test_strip_html_tags() -> None:
    html = "<html lang=en><p>The requested URL <code>/test</code> was not found.</p></html>"
    cleaned = strip_html_tags(html)
    assert "<" not in cleaned
    assert ">" not in cleaned
    assert cleaned == "The requested URL /test was not found."

    trunc = truncate(html, 25)
    assert len(trunc) <= 25
    assert "<" not in trunc

    safe = safe_log_value(html)
    assert "<" not in safe


def test_config_store_read_write() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        store = ConfigStore(root)
        cfg = store.read_config()
        assert cfg.port == 4567

        new_cfg = SleepyRouterConfig(
            port=8080,
            model_groups={"fast": ["model-1"]},
            default_model_group="fast",
        )
        store.write_config(new_cfg)

        reloaded = store.read_config()
        assert reloaded.port == 8080
        assert reloaded.model_groups == {"fast": ["model-1"]}
        assert reloaded.default_model_group == "fast"


def test_config_store_usage_logging() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        store = ConfigStore(root)
        store.append_usage(
            UsageLogEntry(
                ts="2026-08-15T12:00:00Z",
                model="test-model",
                input_tokens=100,
                output_tokens=50,
                success=True,
            )
        )

        logs = store.read_usage_logs()
        assert len(logs) == 1
        assert logs[0].model == "test-model"
        assert logs[0].input_tokens == 100
        assert logs[0].output_tokens == 50
        assert logs[0].success is True
        store.close()


def test_resolve_provider_api_keys() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        env_file = root / ".env"
        env_file.write_text(
            "OPENROUTER_API_KEY=sk-local\n"
            "GOOGLE_API_KEY=google-local\n"
            "ANTIGRAVITY_API_KEY=anti-local\n"
            "FREEBUFF_API_KEY=freebuff-local\n"
        )

        env = {"NVIDIA_API_KEY": "nv-env"}
        keys = resolve_provider_api_keys(env, root)
        assert keys.open_router == "sk-local"
        assert keys.nvidia == "nv-env"
        assert keys.google == "google-local"
        assert keys.antigravity == "anti-local"
        assert keys.freebuff == "freebuff-local"
        assert api_key_for(keys, "freebuff") == "freebuff-local"


def test_resolve_antigravity_auto_refresh_auth_json() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        auth_json_path = root / "auth.json"
        auth_json_path.write_text(
            '{"antigravity": {"refresh": "mock-refresh-token", "access": "", "expires": 0}}'
        )

        with patch(
            "sleepyrouter.config.api_keys.refresh_antigravity_token",
            return_value=("new-refreshed-access-token", 3600),
        ):
            keys = resolve_provider_api_keys({}, root)
            assert keys.antigravity == "new-refreshed-access-token"
            refreshed = force_refresh_antigravity_token(root)
            assert refreshed == "new-refreshed-access-token"


def test_require_any_provider_api_key_raises() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        with pytest.raises(ValueError, match="API 키가 설정되지 않았어요"):
            require_any_provider_api_key({}, root)
