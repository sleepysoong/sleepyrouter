from sleepyrouter.providers import (
    default_provider_registry,
    map_to_litellm_kwargs,
)
from sleepyrouter.providers.antigravity import (
    AntigravityAPIError,
    build_antigravity_payload,
    parse_antigravity_response,
)
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
        id="antigravity/gemini-2.0-flash",
        upstream_id="gemini-2.0-flash",
        provider="antigravity",
        source="antigravity",
    )
    mapped = map_to_litellm_kwargs(model, "test-token", {"temperature": 0.5})
    assert mapped["model"] == "openai/gemini-2.0-flash"
    assert mapped["api_key"] == "test-token"
    assert mapped["api_base"] == "https://cloudcode-pa.googleapis.com"
    assert "headers" in mapped
    assert "antigravity" in mapped["headers"]["User-Agent"]
    assert mapped["reasoning_effort"] == "high"
    assert mapped["thinking"] == {"type": "enabled", "budget_tokens": 32000}


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
    assert payload["model"] == "claude-opus-4-6"
    assert "contents" in payload["request"]
    assert payload["request"]["contents"][0]["role"] == "user"
    assert payload["request"]["contents"][0]["parts"][0]["text"] == "Hello!"
    assert payload["request"]["systemInstruction"]["parts"][0]["text"] == "You are an expert."
    assert payload["request"]["generationConfig"]["temperature"] == 0.3
    assert payload["request"]["generationConfig"]["maxOutputTokens"] == 1000

    dummy_resp = {
        "response": {
            "responseId": "msg-123",
            "candidates": [
                {
                    "content": {
                        "role": "model",
                        "parts": [{"text": "Hello world from Antigravity!"}],
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
    assert parsed["usage"]["prompt_tokens"] == 15
    assert parsed["usage"]["completion_tokens"] == 8


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
