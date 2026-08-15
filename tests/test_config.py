import tempfile
from pathlib import Path

import pytest

from sleepyrouter.config import (
    ConfigStore,
    require_any_provider_api_key,
    resolve_provider_api_keys,
)
from sleepyrouter.types import SleepyRouterConfig, UsageLogEntry
from sleepyrouter.utils import parse_dotenv


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
            "OPENROUTER_API_KEY=sk-local\nGOOGLE_API_KEY=google-local\n"
        )

        env = {"NVIDIA_API_KEY": "nv-env"}
        keys = resolve_provider_api_keys(env, root)
        assert keys.open_router == "sk-local"
        assert keys.nvidia == "nv-env"
        assert keys.google == "google-local"


def test_require_any_provider_api_key_raises() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        with pytest.raises(ValueError, match="API 키가 설정되지 않았어요"):
            require_any_provider_api_key({}, root)
