"""Google Antigravity / CloudCode API HTTP client and streaming pipeline."""

from collections.abc import AsyncGenerator
import json
from typing import Any

import httpx

from .oauth import async_safe_force_refresh_token, resolve_antigravity_project_id
from .serializer import (
    build_antigravity_payload,
    parse_antigravity_response,
    parse_antigravity_sse_chunk,
)

ANTIGRAVITY_BASE_URL = "https://cloudcode-pa.googleapis.com"
ANTIGRAVITY_ENDPOINTS = [
    "https://cloudcode-pa.googleapis.com",
    "https://daily-cloudcode-pa.sandbox.googleapis.com",
]
ANTIGRAVITY_USER_AGENT = "antigravity/1.15.8 linux/amd64"
ANTIGRAVITY_CLIENT_HEADER = "google-cloud-sdk vscode_cloudshelleditor/0.1"


class AntigravityAPIError(Exception):
    """Exception raised for errors returned by the Antigravity API."""

    def __init__(self, status_code: int, message: str) -> None:
        super().__init__(f"Antigravity API error {status_code}: {message}")
        self.status_code = status_code
        self.message = message


class AntigravityClientManager:
    """Manages shared httpx.AsyncClient instance for Antigravity API calls."""

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
        fresh_token = await async_safe_force_refresh_token()
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
    """Executes a single non-streaming completion request against CloudCode endpoints."""
    headers = build_antigravity_headers(api_key)
    project_id = resolve_antigravity_project_id()
    payload = build_antigravity_payload(model_id, request_kwargs, project_id=project_id)
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
        fresh_token = await async_safe_force_refresh_token()
        if fresh_token:
            headers = build_antigravity_headers(fresh_token)
            headers["Accept"] = "text/event-stream"
            resp_stream = client.stream(
                "POST", url, headers=headers, json=payload, timeout=timeout
            )
            resp = await resp_stream.__aenter__()
    return resp, resp_stream


async def _stream_from_single_endpoint(
    client: httpx.AsyncClient,
    endpoint: str,
    headers: dict[str, str],
    payload: dict[str, Any],
    timeout: float,
    model_id: str,
) -> AsyncGenerator[dict[str, Any], None]:
    resp, resp_stream = await _open_antigravity_stream(client, endpoint, headers, payload, timeout)

    if resp.status_code != 200:
        body_bytes = await resp.aread()
        body_str = body_bytes.decode("utf-8", errors="replace")
        await resp_stream.__aexit__(None, None, None)
        err_message = _extract_error_message(body_str)
        raise AntigravityAPIError(resp.status_code, err_message)

    try:
        async for raw_line in resp.aiter_lines():
            cleaned_line = raw_line.strip()
            if not cleaned_line.startswith("data:"):
                continue
            raw_data = cleaned_line[len("data:") :].strip()
            if not raw_data or raw_data == "[DONE]":
                continue
            try:
                chunk_json = json.loads(raw_data)
                if isinstance(chunk_json, dict) and "error" in chunk_json:
                    err_msg = _extract_error_message(raw_data)
                    err_code = int(chunk_json.get("error", {}).get("code") or 500)
                    raise AntigravityAPIError(err_code, err_msg)
                chunk = parse_antigravity_sse_chunk(chunk_json, model_id)
                if chunk is not None:
                    yield chunk
            except json.JSONDecodeError:
                continue
    finally:
        await resp_stream.__aexit__(None, None, None)


async def call_antigravity_stream(
    model_id: str,
    api_key: str,
    request_kwargs: dict[str, Any],
    *,
    timeout: float = 60.0,
) -> AsyncGenerator[dict[str, Any], None]:
    """Streams SSE completion chunks from CloudCode endpoints."""
    headers = build_antigravity_headers(api_key)
    headers["Accept"] = "text/event-stream"
    project_id = resolve_antigravity_project_id()
    payload = build_antigravity_payload(model_id, request_kwargs, project_id=project_id)
    client = get_antigravity_client(timeout)
    yielded_any = False
    last_error: Exception | None = None

    for endpoint in ANTIGRAVITY_ENDPOINTS:
        try:
            async for chunk in _stream_from_single_endpoint(
                client, endpoint, headers, payload, timeout, model_id
            ):
                yielded_any = True
                yield chunk
            if yielded_any:
                return
        except Exception as exc:
            last_error = exc
            if not yielded_any and endpoint != ANTIGRAVITY_ENDPOINTS[-1]:
                continue
            raise

    if last_error:
        raise last_error
    if not yielded_any:
        err_no_out = f"Antigravity stream for {model_id} produced no output"
        raise RuntimeError(err_no_out)
