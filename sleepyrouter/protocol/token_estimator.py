"""Token count estimation."""

import json
from typing import Any


def estimate_input_tokens(body: Any) -> int:
    messages = body.get("messages") or body if isinstance(body, dict) else body
    try:
        text = json.dumps(messages)
    except (TypeError, ValueError):
        text = str(messages)
    return max(1, (len(text) + 3) // 4)
