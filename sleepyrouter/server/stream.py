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
    stream_start = time.time()
    try:
        async for chunk in response_gen:
            if hasattr(chunk, "usage") and chunk.usage:
                input_tokens = getattr(chunk.usage, "prompt_tokens", 0) or 0
                output_tokens = getattr(chunk.usage, "completion_tokens", 0) or 0

            if api_type == "anthropic":
                delta_text = ""
                if hasattr(chunk, "choices") and chunk.choices:
                    delta_text = getattr(chunk.choices[0].delta, "content", "") or ""
                if delta_text:
                    payload = {
                        "type": "content_block_delta",
                        "index": 0,
                        "delta": {"type": "text_delta", "text": delta_text},
                    }
                    yield f"event: content_block_delta\ndata: {json.dumps(payload)}\n\n".encode()
            else:
                chunk_dict = chunk.model_dump() if hasattr(chunk, "model_dump") else dict(chunk)
                yield f"data: {json.dumps(chunk_dict)}\n\n".encode()

        if api_type == "anthropic":
            delta_payload = {
                "type": "message_delta",
                "delta": {"stop_reason": "end_turn", "stop_sequence": None},
                "usage": {"output_tokens": 0},
            }
            yield f"event: message_delta\ndata: {json.dumps(delta_payload)}\n\n".encode()
            yield b'event: message_stop\ndata: {"type":"message_stop"}\n\n'
        else:
            yield b"data: [DONE]\n\n"

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
