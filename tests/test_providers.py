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
