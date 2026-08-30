"""Types module for sleepyrouter."""

from typing import Any

from pydantic import BaseModel, ConfigDict, Field

ModelSource = (
    str  # "openrouter", "nvidia", "copilot", "zen", "google", "antigravity", "freebuff", or custom
)


class ChatMessage(BaseModel):
    model_config = ConfigDict(extra="allow")

    role: str
    content: Any = ""


class ChatCompletionRequest(BaseModel):
    model_config = ConfigDict(extra="allow")

    messages: list[ChatMessage] = Field(min_length=1)
    model: str = ""
    stream: bool = False



class SleepyRouterModel(BaseModel):
    id: str
    upstream_id: str | None = None
    provider: str
    source: ModelSource = "openrouter"
    usage_id: str | None = None
    api_base: str | None = None


def source_of(model: SleepyRouterModel) -> ModelSource:
    return model.source or model.provider or "openrouter"


class UsageLogEntry(BaseModel):
    ts: str
    model: str
    input_tokens: int
    output_tokens: int
    success: bool


class ModelDefinition(BaseModel):
    provider: str
    name: str
    input_price: float | None = None
    output_price: float | None = None
    api_base: str | None = None


class SleepyRouterConfig(BaseModel):
    port: int = 4567
    model_groups: dict[str, list[str]] = Field(default_factory=dict)
    default_model_group: str | None = None
    group_order: list[str] = Field(default_factory=list)
    models: dict[str, ModelDefinition] | None = None


def complete_group_order(groups: dict[str, list[str]], preferred: list[str]) -> list[str]:
    seen = set()
    order: list[str] = []
    for name in preferred:
        if name not in seen and name in groups:
            seen.add(name)
            order.append(name)

    remaining = sorted([n for n in groups if n not in seen])
    return order + remaining
