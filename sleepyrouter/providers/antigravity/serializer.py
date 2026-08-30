"""Serialization and format conversion between OpenAI and Google Antigravity/CloudCode."""

import json
import time
from typing import Any

ANTIGRAVITY_SYSTEM_INSTRUCTION = (
    "You are Antigravity, a powerful agentic AI coding assistant designed by Google DeepMind. "
    "You are pair programming with a user to solve coding tasks. "
    "Be concise, practical, and tool-aware."
)
ANTIGRAVITY_NO_PREAMBLE_INSTRUCTION = (
    "CRITICAL: NEVER output rule checks, formatting guidelines, constraint checklists "
    '(e.g. "No emdashes"), or your thinking/personality preambles in the final response. '
    "Output only the final response."
)

_THINKING_BUDGET_DEFAULT: dict[str, Any] = {
    "thinkingBudget": 32000,
    "includeThoughts": True,
}
_THINKING_LEVEL_HIGH: dict[str, Any] = {"thinkingLevel": "HIGH"}

_STATIC_ROUTING_MAP: dict[str, tuple[str, dict[str, Any]]] = {
    "gemini-3.7-flash": ("gemini-3.7-flash-tiered", _THINKING_LEVEL_HIGH),
    "gemini-3.7-flash-tiered": ("gemini-3.7-flash-tiered", _THINKING_LEVEL_HIGH),
    "gemini-3.7-flash-high": ("gemini-3.7-flash-tiered", _THINKING_LEVEL_HIGH),
    "claude-opus-4.6": ("claude-opus-4-6-thinking", _THINKING_BUDGET_DEFAULT),
    "claude-opus-4-6": ("claude-opus-4-6-thinking", _THINKING_BUDGET_DEFAULT),
    "claude-opus-4-6-thinking": ("claude-opus-4-6-thinking", _THINKING_BUDGET_DEFAULT),
    "claude-sonnet-4.6": ("claude-sonnet-4-6", _THINKING_BUDGET_DEFAULT),
    "claude-sonnet-4-6": ("claude-sonnet-4-6", _THINKING_BUDGET_DEFAULT),
    "gemini-3.6-flash": ("gemini-3.6-flash-high", _THINKING_BUDGET_DEFAULT),
    "gemini-3.6-flash-high": ("gemini-3.6-flash-high", _THINKING_BUDGET_DEFAULT),
    "gemini-3.1-pro": ("gemini-pro-agent", _THINKING_BUDGET_DEFAULT),
    "gemini-pro-agent": ("gemini-pro-agent", _THINKING_BUDGET_DEFAULT),
    "gpt-oss-120b": ("gpt-oss-120b-medium", _THINKING_BUDGET_DEFAULT),
    "gpt-oss-120b-medium": ("gpt-oss-120b-medium", _THINKING_BUDGET_DEFAULT),
}

_CUSTOM_TOOL_SCHEMA_ALLOW = frozenset(
    {"type", "description", "properties", "required", "items", "enum"}
)


def get_runtime_model_and_thinking_config(model_id: str) -> tuple[str, dict[str, Any]]:
    """Resolves upstream model identifier and associated thinking configuration."""
    m = model_id.lower()
    return _STATIC_ROUTING_MAP.get(m, (model_id, _THINKING_BUDGET_DEFAULT))


def _extract_text_content(raw_content: Any) -> str:
    if isinstance(raw_content, list):
        parts = [
            str(p.get("text", ""))
            for p in raw_content
            if isinstance(p, dict) and p.get("type") == "text"
        ]
        return "\n".join(parts)
    return str(raw_content or "")


def _convert_tool_call_part(tc: dict[str, Any]) -> dict[str, Any]:
    fn = tc.get("function", {})
    args = fn.get("arguments", {})
    if isinstance(args, str):
        try:
            args = json.loads(args)
        except (json.JSONDecodeError, TypeError):
            args = {}
    return {
        "functionCall": {
            "name": str(fn.get("name") or ""),
            "args": args,
            "id": str(tc.get("id") or ""),
        }
    }


def _append_tool_response(contents: list[dict[str, Any]], msg: dict[str, Any], text: str) -> None:
    part = {
        "functionResponse": {
            "name": str(msg.get("name") or "tool"),
            "response": {"output": text},
            "id": str(msg.get("tool_call_id") or ""),
        }
    }
    if (
        contents
        and contents[-1].get("role") == "user"
        and any("functionResponse" in p for p in contents[-1].get("parts", []))
    ):
        contents[-1]["parts"].append(part)
    else:
        contents.append({"role": "user", "parts": [part]})


def _convert_assistant_parts(text: str, tool_calls: Any) -> list[dict[str, Any]]:
    parts: list[dict[str, Any]] = []
    if text:
        parts.append({"text": text})
    if isinstance(tool_calls, list):
        parts.extend(_convert_tool_call_part(tc) for tc in tool_calls if isinstance(tc, dict))
    return parts


def _convert_single_message(
    msg: dict[str, Any],
    contents: list[dict[str, Any]],
    system_parts: list[dict[str, str]],
) -> None:
    role = str(msg.get("role", "user"))
    text = _extract_text_content(msg.get("content", ""))

    if role == "system":
        if text:
            system_parts.append({"text": text})
    elif role in ("tool", "toolResult"):
        _append_tool_response(contents, msg, text)
    elif role == "assistant":
        parts = _convert_assistant_parts(text, msg.get("tool_calls"))
        if parts:
            contents.append({"role": "model", "parts": parts})
    else:
        contents.append({"role": "user", "parts": [{"text": text}]})


def _convert_messages_to_contents_and_system(
    messages: list[dict[str, Any]],
) -> tuple[list[dict[str, Any]], list[dict[str, str]]]:
    contents: list[dict[str, Any]] = []
    system_parts: list[dict[str, str]] = [
        {"text": ANTIGRAVITY_SYSTEM_INSTRUCTION},
        {"text": f"Please ignore following [ignore]{ANTIGRAVITY_SYSTEM_INSTRUCTION}[/ignore]"},
        {"text": ANTIGRAVITY_NO_PREAMBLE_INSTRUCTION},
    ]
    for msg in messages:
        _convert_single_message(msg, contents, system_parts)
    return contents, system_parts


def _normalize_custom_tool_type(value: Any) -> str | None:
    if isinstance(value, str):
        return value
    if isinstance(value, list):
        for entry in value:
            if isinstance(entry, str) and entry != "null":
                return entry
    return None


def _clean_schema_field(key: str, value: Any) -> Any:
    res: Any = None
    if key == "type":
        res = _normalize_custom_tool_type(value)
    elif key == "properties" and isinstance(value, dict):
        res = {
            p_name: _normalize_custom_tool_schema(p_schema) for p_name, p_schema in value.items()
        }
    elif key == "items":
        res = _normalize_custom_tool_schema(value)
    elif (key == "description" and isinstance(value, str)) or (
        key == "enum" and isinstance(value, list) and all(isinstance(x, str) for x in value)
    ):
        res = value
    elif key == "required" and isinstance(value, list):
        res = [str(x) for x in value if isinstance(x, (str, int))]
    return res


def _normalize_custom_tool_schema(schema: Any) -> Any:
    if not isinstance(schema, dict):
        return (
            [_normalize_custom_tool_schema(item) for item in schema]
            if isinstance(schema, list)
            else schema
        )
    out: dict[str, Any] = {}
    for key, value in schema.items():
        if key in _CUSTOM_TOOL_SCHEMA_ALLOW:
            cleaned = _clean_schema_field(key, value)
            if cleaned is not None:
                out[key] = cleaned
    return out


def _convert_tools_to_gemini_format(tools: list[Any]) -> list[dict[str, Any]]:
    declarations: list[dict[str, Any]] = []
    for t in tools:
        if not isinstance(t, dict):
            continue
        fn = t.get("function")
        if isinstance(fn, dict) and fn.get("name"):
            decl: dict[str, Any] = {
                "name": fn["name"],
                "description": fn.get("description", ""),
            }
            if "parameters" in fn:
                decl["parameters"] = _normalize_custom_tool_schema(fn["parameters"])
            declarations.append(decl)
    return [{"functionDeclarations": declarations}] if declarations else []


def build_antigravity_payload(
    model_id: str, request_kwargs: dict[str, Any], project_id: str = "lithe-dogfish-7dc4d"
) -> dict[str, Any]:
    """Constructs the JSON payload for CloudCode generateContent API."""
    runtime_model, thinking_config = get_runtime_model_and_thinking_config(model_id)
    messages = request_kwargs.get("messages", [])
    contents, system_parts = _convert_messages_to_contents_and_system(messages)

    gen_config: dict[str, Any] = {"thinkingConfig": thinking_config}
    if "temperature" in request_kwargs:
        gen_config["temperature"] = float(request_kwargs["temperature"])
    if "top_p" in request_kwargs:
        gen_config["topP"] = float(request_kwargs["top_p"])
    if "max_tokens" in request_kwargs:
        gen_config["maxOutputTokens"] = int(request_kwargs["max_tokens"])
    elif "max_completion_tokens" in request_kwargs:
        gen_config["maxOutputTokens"] = int(request_kwargs["max_completion_tokens"])

    budget = thinking_config.get("thinkingBudget")
    if isinstance(budget, int) and budget > 0:
        curr_max = gen_config.get("maxOutputTokens", 0)
        if curr_max <= budget:
            gen_config["maxOutputTokens"] = budget + 8192

    inner_req: dict[str, Any] = {
        "contents": contents,
        "systemInstruction": {
            "role": "user",
            "parts": system_parts,
        },
        "generationConfig": gen_config,
    }

    tools = request_kwargs.get("tools", [])
    if isinstance(tools, list) and tools:
        gemini_tools = _convert_tools_to_gemini_format(tools)
        if gemini_tools:
            inner_req["tools"] = gemini_tools

    return {
        "project": project_id or "lithe-dogfish-7dc4d",
        "model": runtime_model,
        "request": inner_req,
        "requestType": "AGENT",
        "userAgent": "antigravity",
        "requestId": f"slr-{int(time.time() * 1000)}",
    }


def parse_antigravity_response(resp_data: dict[str, Any], model_id: str) -> dict[str, Any]:
    """Parses a CloudCode generateContent JSON response into standard OpenAI ChatCompletion."""
    response_obj = resp_data.get("response", {})
    candidates = response_obj.get("candidates", [])
    text_pieces: list[str] = []
    reasoning_pieces: list[str] = []
    tool_calls: list[dict[str, Any]] = []

    if candidates and isinstance(candidates, list):
        first_candidate = candidates[0]
        if isinstance(first_candidate, dict):
            content_obj = first_candidate.get("content", {})
            for part in content_obj.get("parts", []):
                if not isinstance(part, dict):
                    continue
                if part.get("thought"):
                    reasoning_pieces.append(str(part.get("text") or ""))
                elif "text" in part:
                    text_pieces.append(str(part["text"]))
                elif "functionCall" in part:
                    fc = part["functionCall"]
                    tool_calls.append(
                        {
                            "id": str(fc.get("id") or f"call_{int(time.time() * 1000)}"),
                            "type": "function",
                            "function": {
                                "name": str(fc.get("name") or ""),
                                "arguments": json.dumps(fc.get("args", {}), ensure_ascii=False),
                            },
                        }
                    )

    full_text = "".join(text_pieces)
    full_reasoning = "".join(reasoning_pieces)
    usage_meta = response_obj.get("usageMetadata", {})
    prompt_tokens = usage_meta.get("promptTokenCount", 0)
    completion_tokens = usage_meta.get("candidatesTokenCount", 0)

    message: dict[str, Any] = {"role": "assistant", "content": full_text}
    if full_reasoning:
        message["reasoning_content"] = full_reasoning
    if tool_calls:
        message["tool_calls"] = tool_calls

    return {
        "id": response_obj.get("responseId", f"chatcmpl-ag-{int(time.time())}"),
        "object": "chat.completion",
        "created": int(time.time()),
        "model": model_id,
        "choices": [
            {
                "index": 0,
                "message": message,
                "finish_reason": "tool_calls" if tool_calls else "stop",
            }
        ],
        "usage": {
            "prompt_tokens": prompt_tokens,
            "completion_tokens": completion_tokens,
            "total_tokens": prompt_tokens + completion_tokens,
        },
    }


def _extract_delta_parts(parts: list[Any]) -> tuple[list[str], list[str], list[dict[str, Any]]]:
    texts: list[str] = []
    thoughts: list[str] = []
    tool_calls: list[dict[str, Any]] = []
    for part in parts:
        if not isinstance(part, dict):
            continue
        if part.get("thought"):
            thoughts.append(str(part.get("text") or ""))
        elif "text" in part:
            texts.append(str(part["text"]))
        elif "functionCall" in part:
            fc = part["functionCall"]
            tool_calls.append(
                {
                    "id": str(fc.get("id") or f"call_{int(time.time() * 1000)}"),
                    "type": "function",
                    "function": {
                        "name": str(fc.get("name") or ""),
                        "arguments": json.dumps(fc.get("args", {}), ensure_ascii=False),
                    },
                }
            )
    return texts, thoughts, tool_calls


def _extract_candidate_delta(candidate: dict[str, Any]) -> tuple[dict[str, Any], str | None]:
    finish_reason = candidate.get("finishReason")
    content_obj = candidate.get("content", {})
    parts = content_obj.get("parts", [])
    texts, thoughts, tool_calls = _extract_delta_parts(parts)

    delta: dict[str, Any] = {}
    if texts:
        delta["content"] = "".join(texts)
    if thoughts:
        delta["reasoning_content"] = "".join(thoughts)
    if tool_calls:
        delta["tool_calls"] = tool_calls
        if not finish_reason:
            finish_reason = "tool_calls"
    return delta, finish_reason


def parse_antigravity_sse_chunk(
    chunk_json: dict[str, Any], model_id: str
) -> dict[str, Any] | None:
    """Directly extracts SSE stream chunk delta without full ChatCompletion roundtrip."""
    response_obj = chunk_json.get("response", {})
    candidates = response_obj.get("candidates", [])
    delta: dict[str, Any] = {}
    finish_reason: str | None = None

    if candidates and isinstance(candidates, list) and isinstance(candidates[0], dict):
        delta, finish_reason = _extract_candidate_delta(candidates[0])

    usage_meta = response_obj.get("usageMetadata")
    usage_dict = None
    if isinstance(usage_meta, dict):
        p_tok = usage_meta.get("promptTokenCount", 0)
        c_tok = usage_meta.get("candidatesTokenCount", 0)
        usage_dict = {
            "prompt_tokens": p_tok,
            "completion_tokens": c_tok,
            "total_tokens": p_tok + c_tok,
        }

    if not delta and not finish_reason and not usage_dict:
        return None

    std_finish = finish_reason.lower() if finish_reason else None
    return {
        "id": response_obj.get("responseId", f"chatcmpl-ag-chunk-{int(time.time() * 1000)}"),
        "object": "chat.completion.chunk",
        "created": int(time.time()),
        "model": model_id,
        "choices": [{"index": 0, "delta": delta, "finish_reason": std_finish}],
        "usage": usage_dict,
    }
