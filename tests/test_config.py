from pathlib import Path
import tempfile

from sleepyrouter.config import ConfigStore
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

