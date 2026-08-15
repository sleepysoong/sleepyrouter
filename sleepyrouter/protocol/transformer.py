"""ProtocolTransformer Strategy pattern for request and response transformations."""

from typing import Any, Protocol

from .anthropic_to_openai import anthropic_to_openai
from .openai_to_anthropic import openai_to_anthropic


class ProtocolTransformer(Protocol):
    def transform_request(
        self, body: dict[str, Any], model_id: str, provider_name: str
    ) -> dict[str, Any]: ...

    def transform_response(
        self, response: dict[str, Any], fallback_model: str
    ) -> dict[str, Any]: ...


class AnthropicToOpenAITransformer(ProtocolTransformer):
    def transform_request(
        self, body: dict[str, Any], model_id: str, provider_name: str
    ) -> dict[str, Any]:
        return anthropic_to_openai(body, model_id)

    def transform_response(
        self, response: dict[str, Any], fallback_model: str
    ) -> dict[str, Any]:
        return openai_to_anthropic(response, fallback_model)


class OpenAIIdentityTransformer(ProtocolTransformer):
    def transform_request(
        self, body: dict[str, Any], model_id: str, provider_name: str
    ) -> dict[str, Any]:
        res = dict(body)
        res["model"] = model_id
        return res

    def transform_response(
        self, response: dict[str, Any], fallback_model: str
    ) -> dict[str, Any]:
        return response


class ProtocolTransformerRegistry:
    def __init__(self) -> None:
        self.transformers: dict[str, ProtocolTransformer] = {
            "anthropic": AnthropicToOpenAITransformer(),
            "openai": OpenAIIdentityTransformer(),
        }

    def register(self, api_type: str, transformer: ProtocolTransformer) -> None:
        self.transformers[api_type] = transformer

    def get(self, api_type: str) -> ProtocolTransformer:
        return self.transformers.get(api_type, OpenAIIdentityTransformer())


default_protocol_transformer_registry = ProtocolTransformerRegistry()
