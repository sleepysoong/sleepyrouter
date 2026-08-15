"""Token count estimation."""

import json
from typing import Any


def estimate_input_tokens(body: dict[str, Any]) -> int:
    messages = body.get("messages") or body
    text = json.dumps(messages)
    return max(1, (len(text) + 3) // 4)
