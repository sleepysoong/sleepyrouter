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


def _extract_system_messages(system: Any) -> list[dict[str, Any]]:
    if isinstance(system, str) and system.strip():
        return [{"role": "system", "content": system.strip()}]
    if isinstance(system, list):
        parts = [b.get("text", "") for b in system if isinstance(b, dict) and b.get("text")]
        if parts:
            return [{"role": "system", "content": "\n".join(parts)}]
    return []


def _extract_assistant_message(content: list[Any]) -> dict[str, Any]:
    tool_uses = [b for b in content if isinstance(b, dict) and b.get("type") == "tool_use"]
    thinking_blocks = [b for b in content if isinstance(b, dict) and b.get("type") == "thinking"]
    non_tool_blocks = [b for b in content if isinstance(b, dict) and b.get("type") != "tool_use"]
    text_parts = [b.get("text", "") for b in non_tool_blocks if b.get("type") == "text"]
    thinking_texts = [
        b.get("thinking") or b.get("text", "")
        for b in thinking_blocks
        if b.get("thinking") or b.get("text")
    ]

    tool_calls = []
    for tu in tool_uses:
        tool_input = tu.get("input") or {}
        args = tool_input if isinstance(tool_input, str) else json.dumps(tool_input)
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
    return msg_map


def _extract_content_blocks(role: str, content: list[Any]) -> list[dict[str, Any]]:
    messages: list[dict[str, Any]] = []
    pending_blocks: list[dict[str, Any]] = []

    for block in content:
        if not isinstance(block, dict):
            continue
        btype = block.get("type")
        if btype == "tool_result":
            if pending_blocks:
                text_parts = [b.get("text", "") for b in pending_blocks if b.get("type") == "text"]
                if text_parts:
                    messages.append({"role": role, "content": "\n".join(text_parts)})
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
        text_parts = [b.get("text", "") for b in pending_blocks if b.get("type") == "text"]
        if text_parts:
            messages.append({"role": role, "content": "\n".join(text_parts)})

    return messages


def _extract_openai_tools(tools: Any) -> list[dict[str, Any]]:
    if not isinstance(tools, list) or not tools:
        return []
    return [
        {
            "type": "function",
            "function": {
                "name": t["name"],
                "description": t.get("description"),
                "parameters": t.get("input_schema") or {"type": "object"},
            },
        }
        for t in tools
        if isinstance(t, dict) and t.get("name")
    ]


def anthropic_to_openai(body: dict[str, Any], model_id: str) -> dict[str, Any]:
    messages: list[dict[str, Any]] = _extract_system_messages(body.get("system"))

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

        has_tool_use = any(isinstance(b, dict) and b.get("type") == "tool_use" for b in content)
        if role == "assistant" and has_tool_use:
            messages.append(_extract_assistant_message(content))
        else:
            messages.extend(_extract_content_blocks(role, content))

    result: dict[str, Any] = {
        "model": model_id,
        "messages": messages,
    }

    openai_tools = _extract_openai_tools(body.get("tools"))
    if openai_tools:
        result["tools"] = openai_tools

    for key in ("max_tokens", "temperature", "top_p", "stream"):
        if key in body:
            result[key] = body[key]

    if "stop_sequences" in body:
        result["stop"] = body["stop_sequences"]

    return result
