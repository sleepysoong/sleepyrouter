"""Google Antigravity provider adapter and native client."""

from collections.abc import AsyncGenerator
import json
import time
from typing import Any

import httpx

from sleepyrouter.types import SleepyRouterModel

from .base import (
    BaseProviderAdapter,
    inject_max_reasoning,
)

ANTIGRAVITY_BASE_URL = "https://cloudcode-pa.googleapis.com"
ANTIGRAVITY_USER_AGENT = "antigravity/1.15.8 windows/amd64"
ANTIGRAVITY_CLIENT_HEADER = "google-cloud-sdk vscode_cloudshelleditor/0.1"


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
        res = inject_max_reasoning(
            kwargs,
            effort="high",
            thinking_budget=32000,
        )
        res["model"] = f"openai/{upstream_id}"
        res["api_base"] = ANTIGRAVITY_BASE_URL
        res["api_key"] = api_key
        res["headers"] = {
            "User-Agent": ANTIGRAVITY_USER_AGENT,
            "X-Goog-Api-Client": ANTIGRAVITY_CLIENT_HEADER,
            "Client-Metadata": json.dumps(
                {"ideType": "ANTIGRAVITY", "platform": "MACOS", "pluginType": "GEMINI"}
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
            {"ideType": "ANTIGRAVITY", "platform": "MACOS", "pluginType": "GEMINI"}
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
    return str(raw_content)


def _convert_messages_to_contents_and_system(
    messages: list[dict[str, Any]],
) -> tuple[list[dict[str, Any]], list[dict[str, str]]]:
    contents: list[dict[str, Any]] = []
    system_parts: list[dict[str, str]] = []

    for msg in messages:
        role = str(msg.get("role", "user"))
        text = _extract_text_content(msg.get("content", ""))

        if role == "system":
            system_parts.append({"text": text})
        elif role == "assistant":
            contents.append({"role": "model", "parts": [{"text": text}]})
        else:
            contents.append({"role": "user", "parts": [{"text": text}]})

    return contents, system_parts


def build_antigravity_payload(model_id: str, request_kwargs: dict[str, Any]) -> dict[str, Any]:
    messages = request_kwargs.get("messages", [])
    contents, system_parts = _convert_messages_to_contents_and_system(messages)

    gen_config: dict[str, Any] = {
        "thinkingConfig": {
            "thinkingBudget": 32000,
            "includeThoughts": True,
        }
    }
    if "temperature" in request_kwargs:
        gen_config["temperature"] = float(request_kwargs["temperature"])
    if "top_p" in request_kwargs:
        gen_config["topP"] = float(request_kwargs["top_p"])
    if "max_tokens" in request_kwargs:
        gen_config["maxOutputTokens"] = int(request_kwargs["max_tokens"])
    elif "max_completion_tokens" in request_kwargs:
        gen_config["maxOutputTokens"] = int(request_kwargs["max_completion_tokens"])

    inner_req: dict[str, Any] = {
        "contents": contents,
        "generationConfig": gen_config,
    }
    if system_parts:
        inner_req["systemInstruction"] = {"parts": system_parts}

    return {
        "project": "",
        "model": model_id,
        "request": inner_req,
        "userAgent": "antigravity",
        "requestId": f"slr-{int(time.time() * 1000)}",
    }


def parse_antigravity_response(resp_data: dict[str, Any], model_id: str) -> dict[str, Any]:
    response_obj = resp_data.get("response", {})
    candidates = response_obj.get("candidates", [])
    text_pieces: list[str] = []

    if candidates and isinstance(candidates, list):
        first_candidate = candidates[0]
        if isinstance(first_candidate, dict):
            content_obj = first_candidate.get("content", {})
            parts = content_obj.get("parts", [])
            text_pieces.extend(
                str(part["text"]) for part in parts if isinstance(part, dict) and "text" in part
            )

    full_text = "".join(text_pieces)
    usage_meta = response_obj.get("usageMetadata", {})
    prompt_tokens = usage_meta.get("promptTokenCount", 0)
    completion_tokens = usage_meta.get("candidatesTokenCount", 0)

    return {
        "id": response_obj.get("responseId", f"chatcmpl-ag-{int(time.time())}"),
        "object": "chat.completion",
        "created": int(time.time()),
        "model": model_id,
        "choices": [
            {
                "index": 0,
                "message": {
                    "role": "assistant",
                    "content": full_text,
                },
                "finish_reason": "stop",
            }
        ],
        "usage": {
            "prompt_tokens": prompt_tokens,
            "completion_tokens": completion_tokens,
            "total_tokens": prompt_tokens + completion_tokens,
        },
    }


async def call_antigravity_completion(
    model_id: str,
    api_key: str,
    request_kwargs: dict[str, Any],
    *,
    timeout: float = 60.0,
) -> dict[str, Any]:
    headers = build_antigravity_headers(api_key)
    payload = build_antigravity_payload(model_id, request_kwargs)
    url = f"{ANTIGRAVITY_BASE_URL}/v1internal:generateContent"

    async with httpx.AsyncClient(timeout=timeout) as client:
        resp = await client.post(url, headers=headers, json=payload)
        if resp.status_code != 200:
            try:
                err_json = resp.json()
                err_message = (
                    err_json.get("error", {}).get("message") or err_json.get("message") or resp.text
                )
            except Exception:  # noqa: BLE001
                err_message = resp.text
            raise AntigravityAPIError(resp.status_code, err_message)

        resp_data = resp.json()
        return parse_antigravity_response(resp_data, model_id)


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
    url = f"{ANTIGRAVITY_BASE_URL}/v1internal:streamGenerateContent?alt=sse"

    async with (
        httpx.AsyncClient(timeout=timeout) as client,
        client.stream("POST", url, headers=headers, json=payload) as resp,
    ):
        if resp.status_code != 200:
            body_bytes = await resp.aread()
            body_str = body_bytes.decode("utf-8", errors="replace")
            try:
                err_json = json.loads(body_str)
                err_message = err_json.get("error", {}).get("message") or body_str
            except Exception:  # noqa: BLE001
                err_message = body_str
            raise AntigravityAPIError(resp.status_code, err_message)

        async for raw_line in resp.aiter_lines():
            cleaned_line = raw_line.strip()
            if not cleaned_line.startswith("data:"):
                continue
            raw_data = cleaned_line[len("data:") :].strip()
            if not raw_data or raw_data == "[DONE]":
                continue
            try:
                chunk_json = json.loads(raw_data)
                openai_chunk = parse_antigravity_response(chunk_json, model_id)
                choices = openai_chunk.get("choices", [])
                delta_text = ""
                if choices:
                    delta_text = choices[0].get("message", {}).get("content", "")
                yield {
                    "id": openai_chunk.get("id"),
                    "object": "chat.completion.chunk",
                    "created": int(time.time()),
                    "model": model_id,
                    "choices": [
                        {
                            "index": 0,
                            "delta": {"content": delta_text},
                            "finish_reason": None,
                        }
                    ],
                    "usage": openai_chunk.get("usage"),
                }
            except (json.JSONDecodeError, KeyError):
                continue
