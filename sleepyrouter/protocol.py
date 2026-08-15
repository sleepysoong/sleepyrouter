"""Protocol translation between Anthropic Messages API and OpenAI Chat Completions format."""

import json
import re
import time
from typing import Any

ANTHROPIC_ID_SANITIZER = re.compile(r"[^a-zA-Z0-9_-]")


def sanitize_anthropic_id(val: Any) -> str:
    fallback = f"toolu_{int(time.time() * 1000)}"
    raw = str(val) if val else fallback
    sanitized = ANTHROPIC_ID_SANITIZER.sub("_", raw)
    return sanitized or fallback


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


def estimate_input_tokens(body: dict[str, Any]) -> int:
    messages = body.get("messages") or body
    text = json.dumps(messages)
    return max(1, (len(text) + 3) // 4)


def anthropic_to_openai(body: dict[str, Any], model_id: str) -> dict[str, Any]:
    messages: list[dict[str, Any]] = []

    # System prompt
    system = body.get("system")
    if isinstance(system, str) and system.strip():
        messages.append({"role": "system", "content": system.strip()})
    elif isinstance(system, list):
        parts = [
            b.get("text", "") for b in system if isinstance(b, dict) and b.get("text")
        ]
        if parts:
            messages.append({"role": "system", "content": "\n".join(parts)})

    # Messages
    raw_msgs = body.get("messages") or []
    for raw_msg in raw_msgs:
        if not isinstance(raw_msg, dict):
            continue
        role = str(raw_msg.get("role", ""))
        content = raw_msg.get("content")

        if isinstance(content, str):
            messages.append({"role": role, "content": content})
            continue

        if not isinstance(content, list):
            continue

        tool_uses = [
            b for b in content if isinstance(b, dict) and b.get("type") == "tool_use"
        ]
        thinking_blocks = [
            b for b in content if isinstance(b, dict) and b.get("type") == "thinking"
        ]

        if role == "assistant" and tool_uses:
            non_tool_blocks = [
                b
                for b in content
                if isinstance(b, dict) and b.get("type") != "tool_use"
            ]
            text_parts = [
                b.get("text", "") for b in non_tool_blocks if b.get("type") == "text"
            ]
            thinking_texts = [
                b.get("thinking") or b.get("text", "")
                for b in thinking_blocks
                if b.get("thinking") or b.get("text")
            ]

            tool_calls = []
            for tu in tool_uses:
                tool_input = tu.get("input") or {}
                args = (
                    tool_input
                    if isinstance(tool_input, str)
                    else json.dumps(tool_input)
                )
                tool_calls.append(
                    {
                        "id": sanitize_anthropic_id(tu.get("id")),
                        "type": "function",
                        "function": {
                            "name": str(tu.get("name", "")),
                            "arguments": args,
                        },
                    }
                )

            msg_map: dict[str, Any] = {
                "role": "assistant",
                "content": "\n".join(text_parts) if text_parts else None,
                "tool_calls": tool_calls,
            }
            if thinking_texts:
                msg_map["reasoning_content"] = "\n".join(thinking_texts)
            messages.append(msg_map)
            continue

        pending_blocks: list[dict[str, Any]] = []
        for block in content:
            if not isinstance(block, dict):
                continue
            btype = block.get("type")
            if btype == "tool_result":
                if pending_blocks:
                    text_parts = [
                        b.get("text", "")
                        for b in pending_blocks
                        if b.get("type") == "text"
                    ]
                    if text_parts:
                        messages.append(
                            {"role": role, "content": "\n".join(text_parts)}
                        )
                    pending_blocks = []
                messages.append(
                    {
                        "role": "tool",
                        "tool_call_id": sanitize_anthropic_id(block.get("tool_use_id")),
                        "content": str(block.get("content", "")),
                    }
                )
            elif btype in ("text", "image"):
                pending_blocks.append(block)

        if pending_blocks:
            text_parts = [
                b.get("text", "") for b in pending_blocks if b.get("type") == "text"
            ]
            if text_parts:
                messages.append({"role": role, "content": "\n".join(text_parts)})

    result: dict[str, Any] = {
        "model": model_id,
        "messages": messages,
    }

    # Tools
    tools = body.get("tools")
    if isinstance(tools, list) and tools:
        openai_tools = []
        for t in tools:
            if isinstance(t, dict) and t.get("name"):
                openai_tools.append(
                    {
                        "type": "function",
                        "function": {
                            "name": t["name"],
                            "description": t.get("description"),
                            "parameters": t.get("input_schema") or {"type": "object"},
                        },
                    }
                )
        if openai_tools:
            result["tools"] = openai_tools

    # Common parameters
    for key in ("max_tokens", "temperature", "top_p", "stream"):
        if key in body:
            result[key] = body[key]

    if "stop_sequences" in body:
        result["stop"] = body["stop_sequences"]

    return result


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
