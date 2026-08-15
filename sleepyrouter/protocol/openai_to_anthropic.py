"""OpenAI Chat Completions response to Anthropic Messages format translator."""

import json
import time
from typing import Any

from .anthropic_to_openai import sanitize_anthropic_id


def map_stop_reason(reason: Any) -> str:
    s = str(reason or "")
    if s == "length":
        return "max_tokens"
    elif s in ("tool_calls", "function_call"):
        return "tool_use"
    elif s == "content_filter":
        return "refusal"
    elif s in ("pause_turn", "model_context_window_exceeded"):
        return s
    return "end_turn"


def openai_to_anthropic(
    response: dict[str, Any], fallback_model: str
) -> dict[str, Any]:
    choices = response.get("choices") or []
    choice = choices[0] if choices and isinstance(choices[0], dict) else {}
    msg_obj = choice.get("message") if isinstance(choice, dict) else {}
    message = msg_obj if isinstance(msg_obj, dict) else {}

    content = (
        message.get("content") or choice.get("text") or message.get("refusal") or ""
    )
    reasoning_text = (
        message.get("reasoning_content")
        or message.get("reasoning")
        or message.get("thinking")
        or message.get("thought")
        or ""
    )

    blocks: list[dict[str, Any]] = []

    if reasoning_text:
        blocks.append({"type": "thinking", "thinking": reasoning_text})

    if content:
        blocks.append({"type": "text", "text": str(content)})

    tool_calls = message.get("tool_calls") or []
    for tc in tool_calls:
        if not isinstance(tc, dict):
            continue
        fn = tc.get("function") or {}
        args_raw = fn.get("arguments")
        args = args_raw
        if isinstance(args_raw, str):
            try:
                args = json.loads(args_raw)
            except (json.JSONDecodeError, TypeError, ValueError):
                args = {}

        blocks.append(
            {
                "type": "tool_use",
                "id": sanitize_anthropic_id(tc.get("id")),
                "name": str(fn.get("name", "")),
                "input": args if isinstance(args, dict) else {},
            }
        )

    usage = response.get("usage") or {}
    input_tokens = usage.get("prompt_tokens") or usage.get("input_tokens") or 0
    output_tokens = usage.get("completion_tokens") or usage.get("output_tokens") or 0

    stop_reason = map_stop_reason(choice.get("finish_reason"))
    model = response.get("model") or fallback_model

    return {
        "id": response.get("id") or f"msg_{int(time.time() * 1000)}",
        "type": "message",
        "role": "assistant",
        "content": blocks,
        "model": model,
        "stop_reason": stop_reason,
        "stop_sequence": None,
        "usage": {
            "input_tokens": input_tokens,
            "output_tokens": output_tokens,
        },
    }
