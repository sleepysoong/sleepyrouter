"""Google Antigravity HTTP client, completion, and streaming implementations."""

from collections.abc import AsyncGenerator
import json
import time
from typing import Any

import httpx

from sleepyrouter.config.api_keys import force_refresh_antigravity_token

from .antigravity_models import (
    ANTIGRAVITY_ENDPOINTS,
    AntigravityAPIError,
    build_antigravity_headers,
)
from .antigravity_payload import build_antigravity_payload


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
