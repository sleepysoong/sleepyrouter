from pathlib import Path
import tempfile
from typing import Any
from unittest.mock import patch

import pytest

from sleepyrouter.providers import (
    api_key_for,
    default_provider_registry,
    map_to_litellm_kwargs,
    require_any_provider_api_key,
)
from sleepyrouter.providers.antigravity import (
    AntigravityAPIError,
    build_antigravity_payload,
    get_runtime_model_and_thinking_config,
    parse_antigravity_response,
)
from sleepyrouter.providers.antigravity_oauth import force_refresh_antigravity_token
from sleepyrouter.providers.base import safe_exists
from sleepyrouter.types import SleepyRouterModel


def test_provider_registry_contains_antigravity() -> None:
    adapter = default_provider_registry.get("antigravity")
    assert adapter is not None
    assert adapter.name == "Google Antigravity"
    assert adapter.api_key_env_var == "ANTIGRAVITY_API_KEY"


def test_provider_registry_contains_freebuff() -> None:
    adapter = default_provider_registry.get("freebuff")
    assert adapter is not None
    assert adapter.name == "Freebuff"
    assert adapter.api_key_env_var == "FREEBUFF_API_KEY"


def test_freebuff_litellm_kwargs_mapping() -> None:
    model = SleepyRouterModel(
        id="freebuff/deepseek-v4-pro",
        upstream_id="deepseek-v4-pro",
        provider="freebuff",
        source="freebuff",
    )
    mapped = map_to_litellm_kwargs(model, "test-freebuff-token", {"temperature": 0.7})
    assert mapped["model"] == "openai/deepseek-v4-pro"
    assert mapped["api_key"] == "test-freebuff-token"
    assert mapped["api_base"] == "https://codebuff.com/api/v1"
    assert "headers" in mapped
    assert mapped["headers"]["User-Agent"] == "freebuff/1.0.0"
    assert mapped["reasoning_effort"] == "high"


def test_antigravity_litellm_kwargs_mapping() -> None:
    model = SleepyRouterModel(
        id="antigravity/gemini-3.7-flash",
        upstream_id="gemini-3.7-flash",
        provider="antigravity",
        source="antigravity",
    )
    mapped = map_to_litellm_kwargs(model, "test-token", {"temperature": 0.5})
    assert mapped["model"] == "openai/gemini-3.7-flash-tiered"
    assert mapped["api_key"] == "test-token"
    assert mapped["api_base"] == "https://cloudcode-pa.googleapis.com"
    assert "headers" in mapped
    assert "antigravity" in mapped["headers"]["User-Agent"]
    assert mapped["reasoning_effort"] == "high"
    assert mapped["thinking"] == {"type": "enabled", "budget_tokens": 32000}


def test_antigravity_runtime_model_and_thinking_config() -> None:
    rt, th = get_runtime_model_and_thinking_config("gemini-3.7-flash")
    assert rt == "gemini-3.7-flash-tiered"
    assert th == {"thinkingLevel": "HIGH"}

    rt2, th2 = get_runtime_model_and_thinking_config("claude-opus-4-6")
    assert rt2 == "claude-opus-4-6-thinking"
    assert th2["thinkingBudget"] == 32000

    rt3, th3 = get_runtime_model_and_thinking_config("gemini-3.6-flash")
    assert rt3 == "gemini-3.6-flash-high"
    assert th3["thinkingBudget"] == 32000


def test_antigravity_build_payload_gemini_37_flash_tiered() -> None:
    req_kwargs = {
        "messages": [
            {"role": "system", "content": "You are a senior developer."},
            {"role": "user", "content": "Write quicksort in python."},
        ],
        "temperature": 0.2,
        "max_tokens": 4096,
    }
    payload = build_antigravity_payload("gemini-3.7-flash-tiered", req_kwargs)
    assert payload["model"] == "gemini-3.7-flash-tiered"
    assert payload["requestType"] == "AGENT"
    assert payload["userAgent"] == "antigravity"
    assert "contents" in payload["request"]
    assert payload["request"]["generationConfig"]["thinkingConfig"] == {"thinkingLevel": "HIGH"}
    assert payload["request"]["generationConfig"]["temperature"] == 0.2
    assert payload["request"]["generationConfig"]["maxOutputTokens"] == 4096
    system_parts = [p["text"] for p in payload["request"]["systemInstruction"]["parts"]]
    assert any("You are Antigravity" in p for p in system_parts)
    assert any("You are a senior developer." in p for p in system_parts)


def test_antigravity_build_payload_and_parse_response() -> None:
    req_kwargs = {
        "messages": [
            {"role": "system", "content": "You are an expert."},
            {"role": "user", "content": "Hello!"},
        ],
        "temperature": 0.3,
        "max_tokens": 1000,
    }
    payload = build_antigravity_payload("claude-opus-4-6", req_kwargs)
    assert payload["model"] == "claude-opus-4-6-thinking"
    assert "contents" in payload["request"]
    assert payload["request"]["contents"][0]["role"] == "user"
    assert payload["request"]["contents"][0]["parts"][0]["text"] == "Hello!"
    assert any(
        "You are an expert." in p["text"] for p in payload["request"]["systemInstruction"]["parts"]
    )
    assert payload["request"]["generationConfig"]["temperature"] == 0.3
    # claude-opus-4-6 has thinkingBudget=32000, so maxOutputTokens is bumped to budget + 8192
    assert payload["request"]["generationConfig"]["maxOutputTokens"] == 40192

    dummy_resp = {
        "response": {
            "responseId": "msg-123",
            "candidates": [
                {
                    "content": {
                        "role": "model",
                        "parts": [
                            {"text": "Thinking process...", "thought": True},
                            {"text": "Hello world from Antigravity!"},
                        ],
                    }
                }
            ],
            "usageMetadata": {
                "promptTokenCount": 15,
                "candidatesTokenCount": 8,
            },
        }
    }
    parsed = parse_antigravity_response(dummy_resp, "claude-opus-4-6")
    assert parsed["id"] == "msg-123"
    assert parsed["choices"][0]["message"]["content"] == "Hello world from Antigravity!"
    assert parsed["choices"][0]["message"]["reasoning_content"] == "Thinking process..."
    assert parsed["usage"]["prompt_tokens"] == 15
    assert parsed["usage"]["completion_tokens"] == 8


def test_antigravity_tool_calling_payload_and_parse() -> None:
    req_kwargs = {
        "messages": [
            {"role": "user", "content": "What is the weather?"},
            {
                "role": "assistant",
                "content": "",
                "tool_calls": [
                    {
                        "id": "call_123",
                        "type": "function",
                        "function": {"name": "get_weather", "arguments": '{"city": "Seoul"}'},
                    }
                ],
            },
            {
                "role": "tool",
                "name": "get_weather",
                "tool_call_id": "call_123",
                "content": "Sunny 22C",
            },
        ],
        "tools": [
            {
                "type": "function",
                "function": {
                    "name": "get_weather",
                    "description": "Get weather for city",
                    "parameters": {"type": "object", "properties": {"city": {"type": "string"}}},
                },
            }
        ],
    }
    payload = build_antigravity_payload("claude-opus-4-6", req_kwargs)
    assert "tools" in payload["request"]
    assert payload["request"]["tools"][0]["functionDeclarations"][0]["name"] == "get_weather"

    dummy_tool_resp = {
        "response": {
            "responseId": "msg-tool-1",
            "candidates": [
                {
                    "content": {
                        "role": "model",
                        "parts": [
                            {"text": "Let me look up the weather."},
                            {
                                "functionCall": {
                                    "name": "get_weather",
                                    "args": {"city": "Seoul"},
                                    "id": "call_123",
                                }
                            },
                        ],
                    }
                }
            ],
            "usageMetadata": {"promptTokenCount": 20, "candidatesTokenCount": 10},
        }
    }
    parsed = parse_antigravity_response(dummy_tool_resp, "claude-opus-4-6")
    assert parsed["choices"][0]["message"]["content"] == "Let me look up the weather."
    assert "tool_calls" in parsed["choices"][0]["message"]
    assert parsed["choices"][0]["message"]["tool_calls"][0]["function"]["name"] == "get_weather"
    assert parsed["choices"][0]["finish_reason"] == "tool_calls"


def test_antigravity_schema_normalization_strips_disallowed_fields() -> None:
    req_kwargs = {
        "messages": [{"role": "user", "content": "Run tool"}],
        "tools": [
            {
                "type": "function",
                "function": {
                    "name": "bash_output",
                    "description": "Peek at session output",
                    "parameters": {
                        "type": "object",
                        "properties": {
                            "bash_id": {
                                "type": "string",
                                "description": "Session id",
                                "optional": True,
                            },
                            "view": {
                                "anyOf": [{"const": "log"}, {"const": "screen"}],
                                "description": "View mode",
                            },
                        },
                        "required": ["bash_id"],
                        "additionalProperties": False,
                        "$schema": "http://json-schema.org/draft-07/schema#",
                    },
                },
            }
        ],
    }
    payload = build_antigravity_payload("claude-opus-4-6", req_kwargs)
    params = payload["request"]["tools"][0]["functionDeclarations"][0]["parameters"]
    assert "additionalProperties" not in params
    assert "$schema" not in params
    assert "optional" not in params["properties"]["bash_id"]
    assert "anyOf" not in params["properties"]["view"]
    assert params["properties"]["bash_id"]["type"] == "string"


def test_antigravity_api_error() -> None:
    err = AntigravityAPIError(401, "UNAUTHENTICATED")
    assert err.status_code == 401
    assert "UNAUTHENTICATED" in str(err)


def test_openrouter_max_reasoning_and_thinking() -> None:
    model = SleepyRouterModel(
        id="openrouter/claude-3-7-sonnet",
        upstream_id="anthropic/claude-3.7-sonnet",
        provider="openrouter",
        source="openrouter",
    )
    mapped = map_to_litellm_kwargs(model, "sk-or-test", {})
    assert mapped["model"] == "openrouter/anthropic/claude-3.7-sonnet"
    assert mapped["reasoning_effort"] == "xhigh"
    assert mapped["thinking"] == {"type": "enabled", "budget_tokens": 32000}


def test_nvidia_max_reasoning() -> None:
    model = SleepyRouterModel(
        id="nvidia/deepseek-r1",
        upstream_id="deepseek-ai/deepseek-r1",
        provider="nvidia",
        source="nvidia",
    )
    mapped = map_to_litellm_kwargs(model, "nvapi-test", {})
    assert mapped["reasoning_effort"] == "high"


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


def test_antigravity_oauth_refresh_from_auth_json() -> None:
    with (
        tempfile.TemporaryDirectory() as tmp,
        patch(
            "sleepyrouter.providers.antigravity_oauth.refresh_antigravity_token",
            return_value=("new-refreshed-access-token", 3600),
        ),
    ):
        root = Path(tmp)
        (root / "auth.json").write_text(
            '{"antigravity": {"refresh": "mock-refresh-token", "access": "", "expires": 0}}'
        )
        adapter = default_provider_registry.get("antigravity")
        assert adapter is not None
        assert adapter.get_api_key({}, root) == "new-refreshed-access-token"
        assert force_refresh_antigravity_token(root) == "new-refreshed-access-token"


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
    from unittest.mock import patch

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

    with patch("sleepyrouter.providers.base.acompletion", return_value=MockResp()) as mock_ac:
        res = await adapter.complete(model, "key-1", {"messages": []}, timeout=30.0)
        assert res["id"] == "test-123"
        mock_ac.assert_called_once()

    async def mock_stream_gen():
        yield {"chunk": 1}

    with patch(
        "sleepyrouter.providers.base.acompletion", return_value=mock_stream_gen()
    ) as mock_stream_ac:
        gen = await adapter.stream(model, "key-1", {"messages": []}, timeout=30.0)
        items = [item async for item in gen]
        assert len(items) == 1
        mock_stream_ac.assert_called_once()


@pytest.mark.anyio
async def test_antigravity_provider_adapter_polymorphic_complete_and_stream() -> None:
    adapter = default_provider_registry.get("antigravity")
    assert adapter is not None
    model = SleepyRouterModel(
        id="antigravity/gemini-3.7-flash",
        upstream_id="gemini-3.7-flash",
        provider="antigravity",
        source="antigravity",
    )

    with patch(
        "sleepyrouter.providers.antigravity.call_antigravity_completion",
        return_value={"id": "ag-123"},
    ) as mock_comp:
        res = await adapter.complete(model, "ag-key", {"messages": []})
        assert res["id"] == "ag-123"
        mock_comp.assert_called_once()

    async def mock_ag_stream(*args: Any, **kwargs: Any):
        yield {"id": "chunk-1"}

    with patch(
        "sleepyrouter.providers.antigravity.call_antigravity_stream",
        side_effect=mock_ag_stream,
    ) as mock_stream:
        gen = await adapter.stream(model, "ag-key", {"messages": []})
        items = [item async for item in gen]
        assert len(items) == 1
        mock_stream.assert_called_once()


def test_parse_antigravity_sse_chunk_direct() -> None:
    from sleepyrouter.providers.antigravity import parse_antigravity_sse_chunk

    raw_chunk = {
        "response": {
            "responseId": "test-chunk-id",
            "candidates": [
                {
                    "content": {
                        "parts": [
                            {"thought": True, "text": "Thinking process..."},
                            {"text": "Hello world!"},
                        ]
                    },
                    "finishReason": "STOP",
                }
            ],
            "usageMetadata": {
                "promptTokenCount": 15,
                "candidatesTokenCount": 10,
            },
        }
    }
    chunk = parse_antigravity_sse_chunk(raw_chunk, "gemini-3.7-flash")
    assert chunk is not None
    assert chunk["id"] == "test-chunk-id"
    assert chunk["choices"][0]["delta"]["content"] == "Hello world!"
    assert chunk["choices"][0]["delta"]["reasoning_content"] == "Thinking process..."
    assert chunk["choices"][0]["finish_reason"] == "stop"
    assert chunk["usage"]["prompt_tokens"] == 15
    assert chunk["usage"]["completion_tokens"] == 10


