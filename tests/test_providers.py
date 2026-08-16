from sleepyrouter.providers import (
    default_provider_registry,
    map_to_litellm_kwargs,
)
from sleepyrouter.providers.antigravity import (
    AntigravityAPIError,
    build_antigravity_payload,
    get_runtime_model_and_thinking_config,
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
    assert payload["request"]["generationConfig"]["maxOutputTokens"] == 1000

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
