from sleepyrouter.providers import (
    default_provider_registry,
    map_to_litellm_kwargs,
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
    assert mapped["api_base"] == "https://cloudcode-pa.googleapis.com/v1"
    assert "headers" in mapped
    assert mapped["headers"]["User-Agent"] == "antigravity/1.0.0"
    assert mapped["reasoning_effort"] == "high"
    assert mapped["thinking"] == {"type": "enabled", "budget_tokens": 32000}


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
