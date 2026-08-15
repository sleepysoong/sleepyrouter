"""Anthropic Messages request to OpenAI Chat Completions request translator."""

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
