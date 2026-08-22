from sleepyrouter.protocol import (
    transform_request,
    transform_response,
)


def test_transform_request() -> None:
    body = {
        "model": "high",
        "messages": [{"role": "user", "content": "Hi!"}],
        "temperature": 0.7,
        "max_tokens": 100,
    }
    converted = transform_request(body, "upstream-model")
    assert converted["model"] == "upstream-model"
    assert converted["messages"] == [{"role": "user", "content": "Hi!"}]
    assert converted["temperature"] == 0.7
    assert converted["max_tokens"] == 100


def test_transform_response() -> None:
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

    converted = transform_response(response)
    assert converted == response
