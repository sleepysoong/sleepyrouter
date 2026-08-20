"""ProtocolTransformer: identity pass-through for OpenAI-format requests."""

from typing import Any


def transform_request(body: dict[str, Any], model_id: str) -> dict[str, Any]:
    """Set the model field and pass through."""
    res = dict(body)
    res["model"] = model_id
    return res


def transform_response(response: dict[str, Any]) -> dict[str, Any]:
    """Identity — upstream responses are already OpenAI format."""
    return response
