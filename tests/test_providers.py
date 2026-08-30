"""Tests for provider adapters, registry, and key resolution."""

from pathlib import Path
import tempfile
import time
from typing import Any
from unittest.mock import AsyncMock, patch

import pytest

from sleepyrouter.providers import (
    api_key_for,
    default_provider_registry,
    require_any_provider_api_key,
)
from sleepyrouter.providers.base import safe_exists
from sleepyrouter.types import SleepyRouterModel


def test_provider_registry_contains_freebuff() -> None:
    adapter = default_provider_registry.get("freebuff")
    assert adapter is not None
    assert adapter.name == "Freebuff"
    assert adapter.api_key_env_var == "FREEBUFF_API_KEY"


def test_provider_registry_contains_google() -> None:
    adapter = default_provider_registry.get("google")
    assert adapter is not None
    assert adapter.name == "Google"
    assert adapter.api_key_env_var == "GOOGLE_API_KEY"


def test_freebuff_payload_and_client() -> None:
    adapter = default_provider_registry.get("freebuff")
    assert adapter is not None
    model = SleepyRouterModel(
        id="freebuff/deepseek-v4-pro",
        upstream_id="deepseek-v4-pro",
        provider="freebuff",
        source="freebuff",
        max_effort="high",
    )
    payload = adapter.prepare_payload(model, {"temperature": 0.7})
    assert payload["model"] == "deepseek-v4-pro"
    assert payload["temperature"] == 0.7
    assert payload["reasoning_effort"] == "high"

    client = adapter.get_client("test-freebuff-token")
    assert str(client.base_url) == "https://codebuff.com/api/v1/"
    assert client.default_headers.get("User-Agent") == "freebuff/1.0.0"


def test_openrouter_payload_and_thinking() -> None:
    adapter = default_provider_registry.get("openrouter")
    assert adapter is not None
    model = SleepyRouterModel(
        id="openrouter/claude-3-7-sonnet",
        upstream_id="anthropic/claude-3.7-sonnet",
        provider="openrouter",
        source="openrouter",
        max_effort="xhigh",
        thinking_budget=32000,
    )
    payload = adapter.prepare_payload(model, {})
    assert payload["model"] == "anthropic/claude-3.7-sonnet"
    assert payload["reasoning_effort"] == "xhigh"
    assert payload["thinking"] == {"type": "enabled", "budget_tokens": 32000}


def test_nvidia_payload_and_client() -> None:
    adapter = default_provider_registry.get("nvidia")
    assert adapter is not None
    model = SleepyRouterModel(
        id="nvidia/deepseek-r1",
        upstream_id="deepseek-ai/deepseek-r1",
        provider="nvidia",
        source="nvidia",
        max_effort="high",
    )
    payload = adapter.prepare_payload(model, {})
    assert payload["model"] == "deepseek-ai/deepseek-r1"
    assert payload["reasoning_effort"] == "high"

    client = adapter.get_client("nvapi-test")
    assert str(client.base_url) == "https://integrate.api.nvidia.com/v1/"


def test_get_api_key_env_wins_over_local_dotenv() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        (root / ".env").write_text("NVIDIA_API_KEY=nv-local\n")
        adapter = default_provider_registry.get("nvidia")
        assert adapter is not None
        assert adapter.get_api_key({"NVIDIA_API_KEY": "nv-env"}, root) == "nv-env"
        assert adapter.get_api_key({}, root) == "nv-local"


def test_google_adapter_dual_env_names() -> None:
    with tempfile.TemporaryDirectory() as tmp:
        adapter = default_provider_registry.get("google")
        assert adapter is not None
        assert adapter.get_api_key({"GEMINI_API_KEY": "gk"}, Path(tmp)) == "gk"
        assert adapter.get_api_key({"GOOGLE_API_KEY": "g2"}, Path(tmp)) == "g2"


def test_api_key_for_custom_source_fallback() -> None:
    assert api_key_for("custom-thing", {"CUSTOM_THING_API_KEY": "ck"}) == "ck"


def test_require_any_provider_api_key_raises() -> None:
    with (
        tempfile.TemporaryDirectory() as tmp,
        pytest.raises(ValueError, match="API 키가 설정되지 않았어요"),
    ):
        require_any_provider_api_key({}, Path(tmp))


def test_safe_exists_treats_unreadable_paths_as_missing() -> None:
    """CI regression: pathlib.Path.exists() re-raises EACCES; discovery must not crash."""
    with patch("sleepyrouter.providers.base.os.stat", side_effect=PermissionError(13, "denied")):
        assert safe_exists(Path("/root/.senpi/agent/auth.json")) is False


@pytest.mark.anyio
async def test_base_provider_adapter_complete_and_stream() -> None:
    adapter = default_provider_registry.get("openrouter")
    assert adapter is not None
    model = SleepyRouterModel(
        id="openrouter/gpt-4o",
        upstream_id="gpt-4o",
        provider="openrouter",
        source="openrouter",
    )

    class MockResp:
        def model_dump(self) -> dict[str, Any]:
            return {"id": "test-123", "choices": []}

    mock_create = AsyncMock(return_value=MockResp())
    with patch("openai.resources.chat.completions.AsyncCompletions.create", mock_create):
        res = await adapter.complete(model, "key-1", {"messages": []}, timeout=30.0)
        assert res["id"] == "test-123"
        mock_create.assert_called_once()

    async def mock_stream_gen(*args: Any, **kwargs: Any) -> Any:
        yield {"chunk": 1}

    mock_stream_create = AsyncMock(side_effect=mock_stream_gen)
    with patch(
        "openai.resources.chat.completions.AsyncCompletions.create", mock_stream_create
    ):
        gen = await adapter.stream(model, "key-1", {"messages": []}, timeout=30.0)
        items = [item async for item in gen]
        assert len(items) == 1
        mock_stream_create.assert_called_once()


def test_provider_model_custom_max_effort_and_thinking_budget() -> None:
    adapter = default_provider_registry.get("openrouter")
    assert adapter is not None
    model = SleepyRouterModel(
        id="openrouter/custom-model",
        upstream_id="custom-model",
        provider="openrouter",
        source="openrouter",
        max_effort="low",
        thinking_budget=8000,
    )
    payload = adapter.prepare_payload(model, {"messages": []})
    assert payload["model"] == "custom-model"
    assert payload["reasoning_effort"] == "low"
    assert payload["thinking"] == {"type": "enabled", "budget_tokens": 8000}


def test_copilot_token_cache_thread_safety() -> None:
    import concurrent.futures

    from sleepyrouter.providers.copilot import CopilotTokenCache

    cache = CopilotTokenCache()
    call_count = 0

    class MockResp:
        status_code = 200
        reason_phrase = "OK"

        def json(self) -> dict[str, Any]:
            return {"token": "gh-token-123", "expires_at": time.time() + 3600}

    def mock_get(*args: Any, **kwargs: Any) -> MockResp:
        nonlocal call_count
        call_count += 1
        return MockResp()

    with patch("httpx.Client.get", side_effect=mock_get):
        with concurrent.futures.ThreadPoolExecutor(max_workers=5) as executor:
            tokens = list(executor.map(lambda _: cache.get_token("api-key"), range(10)))

        assert all(t == "gh-token-123" for t in tokens)
        assert call_count == 1
