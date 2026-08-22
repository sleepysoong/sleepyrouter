"""Streaming SSE response generator for OpenAI format."""

from collections.abc import AsyncGenerator
import datetime
import json
import time
from typing import Any

from fastapi.responses import StreamingResponse

from sleepyrouter.config import ConfigStore
from sleepyrouter.events import (
    CandidateFailedEvent,
    CandidateSucceededEvent,
    default_event_bus,
)
from sleepyrouter.types import SleepyRouterModel, UsageLogEntry
from sleepyrouter.utils import format_error_message

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


def _format_openai_sse_event(chunk: Any) -> bytes:
    chunk_dict = chunk.model_dump() if hasattr(chunk, "model_dump") else dict(chunk)
    dumped = json.dumps(chunk_dict, separators=(",", ":"), ensure_ascii=False)
    return f"data: {dumped}\n\n".encode()


async def create_sse_stream_generator(
    response_gen: Any,
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

    try:
        async for chunk in response_gen:
            in_tok, out_tok = _extract_chunk_usage(chunk)
            if in_tok > 0:
                input_tokens = in_tok
            if out_tok > 0:
                output_tokens = out_tok

            delta_text, delta_reasoning = _extract_chunk_content_and_reasoning(chunk)
            accumulated_output_chars += len(delta_text) + len(delta_reasoning)

            yield _format_openai_sse_event(chunk)

        yield OPENAI_DONE_EVENT

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
        err_msg = format_error_message(e)
        default_event_bus.publish(
            CandidateFailedEvent(
                ts=time.time(),
                request_id=request_id,
                index=index,
                total=total,
                model_id=model.usage_id or model.id,
                provider=model.provider,
                duration_sec=duration_sec,
                error_message=f"Stream error: {err_msg}",
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


async def _chain_first_chunk(first_chunk: Any, gen: Any) -> AsyncGenerator[Any, None]:
    if first_chunk is not None:
        yield first_chunk
    async for item in gen:
        yield item


async def start_streaming_response(
    raw_gen: Any,
    model: SleepyRouterModel,
    store: ConfigStore,
    *,
    request_id: int,
    index: int,
    total: int,
) -> StreamingResponse:
    try:
        first_chunk = await anext(raw_gen)
    except StopAsyncIteration:
        err_ended = f"Stream from {model.id} ended before receiving first chunk"
        raise RuntimeError(err_ended) from None

    if first_chunk is None:
        err_empty = f"Stream from {model.id} returned empty first chunk"
        raise RuntimeError(err_empty)

    chained = _chain_first_chunk(first_chunk, raw_gen)
    generator = create_sse_stream_generator(
        chained,
        model,
        store,
        request_id=request_id,
        index=index,
        total=total,
    )
    return StreamingResponse(generator, media_type="text/event-stream")
