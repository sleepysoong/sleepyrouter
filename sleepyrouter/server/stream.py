"""Streaming SSE response generators for Anthropic and OpenAI formats."""

import datetime
import json
from collections.abc import AsyncGenerator
from typing import Any

from sleepyrouter.config import ConfigStore
from sleepyrouter.types import SleepyRouterModel, UsageLogEntry


async def create_sse_stream_generator(
    response_gen: Any,
    api_type: str,
    model: SleepyRouterModel,
    store: ConfigStore,
) -> AsyncGenerator[bytes, None]:
    input_tokens = 0
    output_tokens = 0
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
                    frame = f"event: content_block_delta\ndata: {json.dumps({'type': 'content_block_delta', 'index': 0, 'delta': {'type': 'text_delta', 'text': delta_text}})}\n\n"
                    yield frame.encode()
            else:
                chunk_dict = (
                    chunk.model_dump() if hasattr(chunk, "model_dump") else dict(chunk)
                )
                yield f"data: {json.dumps(chunk_dict)}\n\n".encode()

        if api_type == "anthropic":
            yield b'event: message_delta\ndata: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":0}}\n\n'
            yield b'event: message_stop\ndata: {"type":"message_stop"}\n\n'
        else:
            yield b"data: [DONE]\n\n"

        store.append_usage(
            UsageLogEntry(
                ts=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                model=model.usage_id or model.id,
                input_tokens=input_tokens,
                output_tokens=output_tokens,
                success=True,
            )
        )
    except (RuntimeError, OSError, ValueError):
        store.append_usage(
            UsageLogEntry(
                ts=datetime.datetime.now(datetime.timezone.utc).isoformat(),
                model=model.usage_id or model.id,
                input_tokens=0,
                output_tokens=0,
                success=False,
            )
        )
