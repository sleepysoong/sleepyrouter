from sleepyrouter.protocol import (
    anthropic_to_openai,
    estimate_input_tokens,
    map_stop_reason,
    openai_to_anthropic,
)


def test_map_stop_reason():
    assert map_stop_reason("length") == "max_tokens"
    assert map_stop_reason("tool_calls") == "tool_use"
    assert map_stop_reason("function_call") == "tool_use"
    assert map_stop_reason("content_filter") == "refusal"
    assert map_stop_reason("stop") == "end_turn"
    assert map_stop_reason("unknown") == "end_turn"


def test_estimate_input_tokens():
    body = {"messages": [{"role": "user", "content": "Hello world"}]}
    assert estimate_input_tokens(body) > 0


def test_anthropic_to_openai():
    body = {
        "model": "claude-3-5-sonnet",
        "system": "You are a helpful assistant.",
        "messages": [{"role": "user", "content": "Hi!"}],
        "temperature": 0.7,
        "max_tokens": 100,
    }
    converted = anthropic_to_openai(body, "upstream-model")
    assert converted["model"] == "upstream-model"
    assert len(converted["messages"]) == 2
    assert converted["messages"][0] == {
        "role": "system",
        "content": "You are a helpful assistant.",
    }
    assert converted["messages"][1] == {"role": "user", "content": "Hi!"}
    assert converted["temperature"] == 0.7
    assert converted["max_tokens"] == 100


def test_openai_to_anthropic():
    response = {
        "id": "chatcmpl-999",
        "model": "gpt-4o",
        "choices": [
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": "Hello there!",
                    "reasoning_content": "Thinking steps",
                },
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 10, "completion_tokens": 5},
    }

    converted = openai_to_anthropic(response, "fallback-model")
    assert converted["id"] == "chatcmpl-999"
    assert converted["type"] == "message"
    assert converted["role"] == "assistant"
    assert converted["model"] == "gpt-4o"
    assert converted["stop_reason"] == "end_turn"
    assert len(converted["content"]) == 2
    assert converted["content"][0] == {"type": "thinking", "thinking": "Thinking steps"}
    assert converted["content"][1] == {"type": "text", "text": "Hello there!"}
    assert converted["usage"] == {"input_tokens": 10, "output_tokens": 5}
