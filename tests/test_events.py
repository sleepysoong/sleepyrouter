from sleepyrouter.events import (
    CandidateFailedEvent,
    EventBus,
)
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
