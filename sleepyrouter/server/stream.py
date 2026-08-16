"""Streaming SSE response generators for Anthropic and OpenAI formats."""

from collections.abc import AsyncGenerator
import datetime
import json
import time
from typing import Any

from sleepyrouter.config import ConfigStore
from sleepyrouter.events import (
    CandidateFailedEvent,
    CandidateSucceededEvent,
    default_event_bus,
)
from sleepyrouter.types import SleepyRouterModel, UsageLogEntry

ANTHROPIC_STOP_EVENT = (
    b"event: message_delta\n"
    b'data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},'
    b'"usage":{"output_tokens":0}}\n\n'
    b'event: message_stop\ndata: {"type":"message_stop"}\n\n'
)
OPENAI_DONE_EVENT = b"data: [DONE]\n\n"


def _extract_chunk_usage(chunk: Any) -> tuple[int, int]:
    if isinstance(chunk, dict):
        usage = chunk.get("usage")
        if isinstance(usage, dict):
            return int(usage.get("prompt_tokens") or 0), int(usage.get("completion_tokens") or 0)
    elif hasattr(chunk, "usage") and chunk.usage:
        return int(getattr(chunk.usage, "prompt_tokens", 0) or 0), int(
            getattr(chunk.usage, "completion_tokens", 0) or 0
        )
    return 0, 0


def _extract_chunk_content_and_reasoning(chunk: Any) -> tuple[str, str]:
    if isinstance(chunk, dict):
        choices = chunk.get("choices", [])
        if choices and isinstance(choices[0], dict):
            delta = choices[0].get("delta", {})
            return str(delta.get("content") or ""), str(delta.get("reasoning_content") or "")
    elif hasattr(chunk, "choices") and chunk.choices:
        first_choice = chunk.choices[0]
        delta = getattr(first_choice, "delta", None)
        if delta:
            content = getattr(delta, "content", "") or ""
            reasoning = (
                getattr(delta, "reasoning_content", "") or getattr(delta, "reasoning", "") or ""
            )
            return str(content), str(reasoning)
    return "", ""


def _format_anthropic_sse_events(
    delta_text: str, delta_reasoning: str, *, has_thinking: bool
) -> tuple[list[bytes], bool]:
    events: list[bytes] = []
    if delta_reasoning:
        has_thinking = True
        thinking_payload = {
            "type": "content_block_delta",
            "index": 0,
            "delta": {"type": "thinking_delta", "thinking": delta_reasoning},
        }
        dumped = json.dumps(thinking_payload, separators=(",", ":"), ensure_ascii=False)
        events.append(f"event: content_block_delta\ndata: {dumped}\n\n".encode())

    if delta_text:
        block_idx = 1 if has_thinking else 0
        payload = {
            "type": "content_block_delta",
            "index": block_idx,
            "delta": {"type": "text_delta", "text": delta_text},
        }
        dumped = json.dumps(payload, separators=(",", ":"), ensure_ascii=False)
        events.append(f"event: content_block_delta\ndata: {dumped}\n\n".encode())

    return events, has_thinking


def _format_openai_sse_event(chunk: Any) -> bytes:
    chunk_dict = chunk.model_dump() if hasattr(chunk, "model_dump") else dict(chunk)
    dumped = json.dumps(chunk_dict, separators=(",", ":"), ensure_ascii=False)
    return f"data: {dumped}\n\n".encode()


async def create_sse_stream_generator(
    response_gen: Any,
    api_type: str,
    model: SleepyRouterModel,
    store: ConfigStore,
    *,
    request_id: int = 0,
    index: int = 1,
    total: int = 1,
) -> AsyncGenerator[bytes, None]:
    input_tokens = 0
    output_tokens = 0
    accumulated_output_chars = 0
    stream_start = time.time()
    has_thinking_block = False

    try:
        async for chunk in response_gen:
            in_tok, out_tok = _extract_chunk_usage(chunk)
            if in_tok > 0:
                input_tokens = in_tok
            if out_tok > 0:
                output_tokens = out_tok

            delta_text, delta_reasoning = _extract_chunk_content_and_reasoning(chunk)
            accumulated_output_chars += len(delta_text) + len(delta_reasoning)

            if api_type == "anthropic":
                ev_list, has_thinking_block = _format_anthropic_sse_events(
                    delta_text, delta_reasoning, has_thinking=has_thinking_block
                )
                for ev in ev_list:
                    yield ev
            else:
                yield _format_openai_sse_event(chunk)

        yield ANTHROPIC_STOP_EVENT if api_type == "anthropic" else OPENAI_DONE_EVENT

        if output_tokens == 0 and accumulated_output_chars > 0:
            output_tokens = max(1, accumulated_output_chars // 4)

        duration_sec = time.time() - stream_start
        default_event_bus.publish(
            CandidateSucceededEvent(
                ts=time.time(),
                request_id=request_id,
                index=index,
                total=total,
                model_id=model.usage_id or model.id,
                provider=model.provider,
                duration_sec=duration_sec,
                input_tokens=input_tokens,
                output_tokens=output_tokens,
            )
        )

        store.append_usage(
            UsageLogEntry(
                ts=datetime.datetime.now(datetime.UTC).isoformat(),
                model=model.usage_id or model.id,
                input_tokens=input_tokens,
                output_tokens=output_tokens,
                success=True,
            )
        )
    except Exception as e:  # noqa: BLE001
        duration_sec = time.time() - stream_start
        default_event_bus.publish(
            CandidateFailedEvent(
                ts=time.time(),
                request_id=request_id,
                index=index,
                total=total,
                model_id=model.usage_id or model.id,
                provider=model.provider,
                duration_sec=duration_sec,
                error_message=f"Stream error: {e}",
            )
        )
        store.append_usage(
            UsageLogEntry(
                ts=datetime.datetime.now(datetime.UTC).isoformat(),
                model=model.usage_id or model.id,
                input_tokens=0,
                output_tokens=0,
                success=False,
            )
        )
