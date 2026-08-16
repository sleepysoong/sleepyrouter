from pathlib import Path
from unittest.mock import patch

from sleepyrouter.events import (
    AllCandidatesFailedEvent,
    CandidateFailedEvent,
    EventBus,
    FailoverEvent,
    notify_discord_on_all_failed,
    notify_discord_on_failover,
    notify_discord_on_failure,
)
from sleepyrouter.events.discord import get_webhook_url
from sleepyrouter.providers import map_to_litellm_kwargs
from sleepyrouter.types import ModelDefinition, SleepyRouterModel


def test_custom_api_base_in_model_definition() -> None:
    def_obj = ModelDefinition(
        provider="ollama",
        name="llama3.1",
        api_base="http://localhost:11434/v1",
    )
    model = SleepyRouterModel(
        id="ollama/llama3.1",
        upstream_id=def_obj.name,
        provider=def_obj.provider,
        source=def_obj.provider,
        api_base=def_obj.api_base,
    )
    mapped = map_to_litellm_kwargs(model, "sk-test", {})
    assert mapped["model"] == "openai/llama3.1"
    assert mapped["api_base"] == "http://localhost:11434/v1"


def test_event_bus_publish_and_subscribe() -> None:
    bus = EventBus()
    received_events = []

    def handle_failed(evt: CandidateFailedEvent) -> None:
        received_events.append(evt)

    bus.subscribe(CandidateFailedEvent, handle_failed)
    bus.publish(
        CandidateFailedEvent(
            ts=123456.0,
            request_id=1,
            model_id="test-model",
            provider="test-provider",
            error_message="Test error",
        )
    )

    assert len(received_events) == 1
    assert received_events[0].model_id == "test-model"
    assert received_events[0].error_message == "Test error"


@patch("requests.post")
def test_discord_notify_on_failover(mock_post: object) -> None:
    with patch.dict(
        "os.environ", {"DISCORD_WEBHOOK_URL": "https://discord.com/api/webhooks/test"}
    ):
        evt = FailoverEvent(
            ts=123456.0,
            request_id=1,
            failed_model_id="openrouter/free-1",
            next_model_id="nvidia/free-2",
            provider="openrouter",
            error_message="Rate limit 429: Too Many Requests",
        )
        notify_discord_on_failover(evt)

        assert mock_post.called  # type: ignore[attr-defined]
        args, kwargs = mock_post.call_args  # type: ignore[attr-defined]
        assert args[0] == "https://discord.com/api/webhooks/test"
        payload = kwargs["json"]
        assert "다음 후보 모델 전환" in payload["content"]
        assert "openrouter/free-1" in payload["content"]
        assert "nvidia/free-2" in payload["content"]
        assert "Rate limit 429" in payload["content"]


@patch("requests.post")
def test_discord_notify_on_all_failed(mock_post: object) -> None:
    with patch.dict("os.environ", {"WEBHOOK_URL": "https://custom-webhook.com/alert"}):
        evt = AllCandidatesFailedEvent(
            ts=123456.0,
            request_id=1,
            candidates_tried=["model-1", "model-2"],
            last_error="All upstreams unavailable 503",
        )
        notify_discord_on_all_failed(evt)

        assert mock_post.called  # type: ignore[attr-defined]
        args, kwargs = mock_post.call_args  # type: ignore[attr-defined]
        assert args[0] == "https://custom-webhook.com/alert"
        payload = kwargs["json"]
        assert "모든 후보 모델 호출 실패" in payload["content"]
        assert "model-1" in payload["content"]
        assert "model-2" in payload["content"]


@patch("requests.post")
def test_discord_notify_on_failure(mock_post: object) -> None:
    with patch.dict(
        "os.environ", {"DISCORD_WEBHOOK_URL": "https://discord.com/api/webhooks/test"}
    ):
        evt = CandidateFailedEvent(
            ts=123456.0,
            request_id=1,
            model_id="google/gemini-2.0",
            provider="google",
            error_message="ResourceExhausted quota exceeded",
        )
        notify_discord_on_failure(evt)

        assert mock_post.called  # type: ignore[attr-defined]
        payload = mock_post.call_args[1]["json"]  # type: ignore[attr-defined]
        assert "모델 호출 실패" in payload["content"]
        assert "google/gemini-2.0" in payload["content"]
        assert "ResourceExhausted" in payload["content"]


def test_get_webhook_url_fallback(tmp_path: Path) -> None:
    env_file = tmp_path / ".env"
    env_file.write_text(
        "DISCORD_WEBHOOK_URL=https://discord.com/api/webhooks/file_test\n"
    )
    with (
        patch.dict("os.environ", {}, clear=True),
        patch("sleepyrouter.events.discord.get_config_root", return_value=tmp_path),
    ):
        assert get_webhook_url() == "https://discord.com/api/webhooks/file_test"
