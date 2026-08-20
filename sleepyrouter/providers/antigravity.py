"""Google Antigravity provider adapter and native client."""

from collections.abc import AsyncGenerator
import json
import os
from pathlib import Path
import time
from typing import Any

import httpx

from sleepyrouter.config.api_keys import force_refresh_antigravity_token
from sleepyrouter.types import SleepyRouterModel

from .base import (
    BaseProviderAdapter,
    inject_max_reasoning,
)

ANTIGRAVITY_BASE_URL = "https://cloudcode-pa.googleapis.com"
ANTIGRAVITY_ENDPOINTS = [
    "https://cloudcode-pa.googleapis.com",
    "https://daily-cloudcode-pa.sandbox.googleapis.com",
]
ANTIGRAVITY_USER_AGENT = "antigravity/1.15.8 linux/amd64"
ANTIGRAVITY_CLIENT_HEADER = "google-cloud-sdk vscode_cloudshelleditor/0.1"

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


def resolve_antigravity_project_id() -> str:
    env_proj = os.environ.get("ANTIGRAVITY_PROJECT_ID", "").strip()
    if env_proj:
        return env_proj

    auth_candidates = [
        Path.home() / ".senpi" / "agent" / "auth.json",
        Path("/root/.senpi/agent/auth.json"),
    ]
    for p in auth_candidates:
        if p.exists():
            try:
                data = json.loads(p.read_text(encoding="utf-8"))
                proj = data.get("antigravity", {}).get("projectId")
                if isinstance(proj, str) and proj.strip():
                    return proj.strip()
            except (OSError, json.JSONDecodeError):
                pass
    return "lithe-dogfish-7dc4d"


def get_runtime_model_and_thinking_config(
    model_id: str,
) -> tuple[str, dict[str, Any]]:
    m = model_id.lower()
    if m in _STATIC_ROUTING_MAP:
        return _STATIC_ROUTING_MAP[m]
    return model_id, _THINKING_BUDGET_DEFAULT


class AntigravityClientManager:
    def __init__(self) -> None:
        self._client: httpx.AsyncClient | None = None

    def get_client(self, timeout: float = 60.0) -> httpx.AsyncClient:
        if self._client is None or self._client.is_closed:
            self._client = httpx.AsyncClient(
                timeout=timeout,
                limits=httpx.Limits(
                    max_keepalive_connections=50,
                    max_connections=200,
                    keepalive_expiry=30.0,
                ),
            )
        return self._client


_client_manager = AntigravityClientManager()


def get_antigravity_client(timeout: float = 60.0) -> httpx.AsyncClient:
    return _client_manager.get_client(timeout)


class AntigravityAPIError(Exception):
    """Exception raised for errors returned by the Antigravity API."""

    def __init__(self, status_code: int, message: str) -> None:
        super().__init__(f"Antigravity API error {status_code}: {message}")
        self.status_code = status_code
        self.message = message


class AntigravityProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="Google Antigravity",
            source="antigravity",
            api_key_env_var="ANTIGRAVITY_API_KEY",
            message_protocol="openai",
            default_reasoning_effort="high",
            default_thinking_budget=32000,
        )

    def map_litellm_kwargs(
        self, model: SleepyRouterModel, api_key: str, kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = model.upstream_id or model.id
        runtime_id, _ = get_runtime_model_and_thinking_config(upstream_id)
        res = inject_max_reasoning(
            kwargs,
            effort="high",
            thinking_budget=32000,
        )
        res["model"] = f"openai/{runtime_id}"
        res["api_base"] = ANTIGRAVITY_BASE_URL
        res["api_key"] = api_key
        res["headers"] = {
            "User-Agent": ANTIGRAVITY_USER_AGENT,
            "X-Goog-Api-Client": ANTIGRAVITY_CLIENT_HEADER,
            "Client-Metadata": json.dumps(
                {
                    "ideType": "ANTIGRAVITY",
                    "platform": "PLATFORM_UNSPECIFIED",
                    "pluginType": "GEMINI",
                }
            ),
        }
        return res


def build_antigravity_headers(api_key: str) -> dict[str, str]:
    return {
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
        "User-Agent": ANTIGRAVITY_USER_AGENT,
        "X-Goog-Api-Client": ANTIGRAVITY_CLIENT_HEADER,
        "Client-Metadata": json.dumps(
            {
                "ideType": "ANTIGRAVITY",
                "platform": "PLATFORM_UNSPECIFIED",
                "pluginType": "GEMINI",
            }
        ),
    }


def _extract_text_content(raw_content: Any) -> str:
    if isinstance(raw_content, list):
        text_parts = [
            str(p.get("text", ""))
            for p in raw_content
            if isinstance(p, dict) and p.get("type") == "text"
        ]
        return "\n".join(text_parts)
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
        {"text": (f"Please ignore following [ignore]{ANTIGRAVITY_SYSTEM_INSTRUCTION}[/ignore]")},
        {"text": ANTIGRAVITY_NO_PREAMBLE_INSTRUCTION},
    ]

    for msg in messages:
        _convert_single_message(msg, contents, system_parts)

    return contents, system_parts


_CUSTOM_TOOL_SCHEMA_ALLOW = frozenset(
    {"type", "description", "properties", "required", "items", "enum"}
)


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
            prop_name: _normalize_custom_tool_schema(prop_schema)
            for prop_name, prop_schema in value.items()
        }
    elif key in ("items", "description"):
        res = (
            _normalize_custom_tool_schema(value)
            if key == "items"
            else (value if isinstance(value, str) else None)
        )
    elif key in ("enum", "required") and isinstance(value, list):
        if key == "enum" and all(isinstance(x, str) for x in value):
            res = value
        elif key == "required":
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
    if declarations:
        return [{"functionDeclarations": declarations}]
    return []


def build_antigravity_payload(model_id: str, request_kwargs: dict[str, Any]) -> dict[str, Any]:
    runtime_model, thinking_config = get_runtime_model_and_thinking_config(model_id)
    messages = request_kwargs.get("messages", [])
    contents, system_parts = _convert_messages_to_contents_and_system(messages)

    gen_config: dict[str, Any] = {
        "thinkingConfig": thinking_config,
    }
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
        "project": resolve_antigravity_project_id(),
        "model": runtime_model,
        "request": inner_req,
        "requestType": "AGENT",
        "userAgent": "antigravity",
        "requestId": f"slr-{int(time.time() * 1000)}",
    }


def parse_antigravity_response(resp_data: dict[str, Any], model_id: str) -> dict[str, Any]:
    response_obj = resp_data.get("response", {})
    candidates = response_obj.get("candidates", [])
    text_pieces: list[str] = []
    reasoning_pieces: list[str] = []
    tool_calls: list[dict[str, Any]] = []

    if candidates and isinstance(candidates, list):
        first_candidate = candidates[0]
        if isinstance(first_candidate, dict):
            content_obj = first_candidate.get("content", {})
            parts = content_obj.get("parts", [])
            for part in parts:
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

    message: dict[str, Any] = {
        "role": "assistant",
        "content": full_text,
    }
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


def _extract_error_message(text: str) -> str:
    try:
        err_json = json.loads(text)
        return str(err_json.get("error", {}).get("message") or err_json.get("message") or text)
    except Exception:  # noqa: BLE001
        return text


async def _execute_antigravity_post(
    client: httpx.AsyncClient,
    endpoint: str,
    headers: dict[str, str],
    payload: dict[str, Any],
    timeout: float,
) -> tuple[httpx.Response, dict[str, str]]:
    url = f"{endpoint}/v1internal:generateContent"
    resp = await client.post(url, headers=headers, json=payload, timeout=timeout)
    if resp.status_code == 401:
        fresh_token = force_refresh_antigravity_token()
        if fresh_token:
            headers = build_antigravity_headers(fresh_token)
            resp = await client.post(url, headers=headers, json=payload, timeout=timeout)
    return resp, headers


async def call_antigravity_completion(
    model_id: str,
    api_key: str,
    request_kwargs: dict[str, Any],
    *,
    timeout: float = 60.0,
) -> dict[str, Any]:
    headers = build_antigravity_headers(api_key)
    payload = build_antigravity_payload(model_id, request_kwargs)
    client = get_antigravity_client(timeout)

    last_error: Exception | None = None
    for endpoint in ANTIGRAVITY_ENDPOINTS:
        try:
            resp, headers = await _execute_antigravity_post(
                client, endpoint, headers, payload, timeout
            )
            if resp.status_code == 200:
                resp_data = resp.json()
                return parse_antigravity_response(resp_data, model_id)
            err_message = _extract_error_message(resp.text)
            last_error = AntigravityAPIError(resp.status_code, err_message)
            if resp.status_code not in (429, 500, 502, 503, 504):
                raise last_error
        except (httpx.RequestError, AntigravityAPIError) as exc:
            last_error = exc
            continue

    if last_error:
        raise last_error
    raise AntigravityAPIError(500, "All Antigravity endpoints failed")


def _parse_sse_chunk(raw_data: str, model_id: str) -> dict[str, Any] | None:
    try:
        chunk_json = json.loads(raw_data)
        openai_chunk = parse_antigravity_response(chunk_json, model_id)
        choices = openai_chunk.get("choices", [])
        delta: dict[str, Any] = {}
        if choices:
            msg = choices[0].get("message", {})
            if msg.get("content"):
                delta["content"] = msg["content"]
            if msg.get("reasoning_content"):
                delta["reasoning_content"] = msg["reasoning_content"]
            if msg.get("tool_calls"):
                delta["tool_calls"] = msg["tool_calls"]
        return {
            "id": openai_chunk.get("id"),
            "object": "chat.completion.chunk",
            "created": int(time.time()),
            "model": model_id,
            "choices": [
                {
                    "index": 0,
                    "delta": delta,
                    "finish_reason": choices[0].get("finish_reason") if choices else None,
                }
            ],
            "usage": openai_chunk.get("usage"),
        }
    except (json.JSONDecodeError, KeyError):
        return None


async def _open_antigravity_stream(
    client: httpx.AsyncClient,
    endpoint: str,
    headers: dict[str, str],
    payload: dict[str, Any],
    timeout: float,
) -> tuple[httpx.Response, Any]:
    url = f"{endpoint}/v1internal:streamGenerateContent?alt=sse"
    resp_stream = client.stream("POST", url, headers=headers, json=payload, timeout=timeout)
    resp = await resp_stream.__aenter__()

    if resp.status_code == 401:
        await resp_stream.__aexit__(None, None, None)
        fresh_token = force_refresh_antigravity_token()
        if fresh_token:
            headers = build_antigravity_headers(fresh_token)
            headers["Accept"] = "text/event-stream"
            resp_stream = client.stream("POST", url, headers=headers, json=payload, timeout=timeout)
            resp = await resp_stream.__aenter__()
    return resp, resp_stream


async def call_antigravity_stream(
    model_id: str,
    api_key: str,
    request_kwargs: dict[str, Any],
    *,
    timeout: float = 60.0,
) -> AsyncGenerator[dict[str, Any], None]:
    headers = build_antigravity_headers(api_key)
    headers["Accept"] = "text/event-stream"
    payload = build_antigravity_payload(model_id, request_kwargs)
    client = get_antigravity_client(timeout)

    for endpoint in ANTIGRAVITY_ENDPOINTS:
        resp, resp_stream = await _open_antigravity_stream(
            client, endpoint, headers, payload, timeout
        )

        if resp.status_code != 200:
            body_bytes = await resp.aread()
            body_str = body_bytes.decode("utf-8", errors="replace")
            await resp_stream.__aexit__(None, None, None)
            err_message = _extract_error_message(body_str)
            if (
                resp.status_code in (429, 500, 502, 503, 504)
                and endpoint != ANTIGRAVITY_ENDPOINTS[-1]
            ):
                continue
            raise AntigravityAPIError(resp.status_code, err_message)

        try:
            async for raw_line in resp.aiter_lines():
                cleaned_line = raw_line.strip()
                if not cleaned_line.startswith("data:"):
                    continue
                raw_data = cleaned_line[len("data:") :].strip()
                if not raw_data or raw_data == "[DONE]":
                    continue
                chunk = _parse_sse_chunk(raw_data, model_id)
                if chunk is not None:
                    yield chunk
            return
        finally:
            await resp_stream.__aexit__(None, None, None)
