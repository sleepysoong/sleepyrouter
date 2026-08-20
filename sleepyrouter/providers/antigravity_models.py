"""Google Antigravity provider constants, routing configuration, and adapter."""

import json
import os
from pathlib import Path
from typing import Any

from sleepyrouter.types import SleepyRouterModel

from .base import BaseProviderAdapter, inject_max_reasoning

ANTIGRAVITY_BASE_URL = "https://cloudcode-pa.googleapis.com"
ANTIGRAVITY_ENDPOINTS = [
    "https://cloudcode-pa.googleapis.com",
    "https://daily-cloudcode-pa.sandbox.googleapis.com",
]
ANTIGRAVITY_USER_AGENT = "antigravity/1.15.8 linux/amd64"
ANTIGRAVITY_CLIENT_HEADER = "google-cloud-sdk vscode_cloudshelleditor/0.1"

ANTIGRAVITY_SYSTEM_INSTRUCTION = (
    "You are Antigravity, a powerful agentic AI coding assistant designed by Google DeepMind. "
    "You are pair programming with a user to solve coding tasks. "
    "Be concise, practical, and tool-aware."
)
ANTIGRAVITY_NO_PREAMBLE_INSTRUCTION = (
    "CRITICAL: NEVER output rule checks, formatting guidelines, constraint checklists "
    '(e.g. "No emdashes"), or your thinking/personality preambles in the final response. '
    "Output only the final response."
)

_THINKING_BUDGET_DEFAULT: dict[str, Any] = {
    "thinkingBudget": 32000,
    "includeThoughts": True,
}
_THINKING_LEVEL_HIGH: dict[str, Any] = {"thinkingLevel": "HIGH"}

_STATIC_ROUTING_MAP: dict[str, tuple[str, dict[str, Any]]] = {
    "gemini-3.7-flash": ("gemini-3.7-flash-tiered", _THINKING_LEVEL_HIGH),
    "gemini-3.7-flash-tiered": ("gemini-3.7-flash-tiered", _THINKING_LEVEL_HIGH),
    "gemini-3.7-flash-high": ("gemini-3.7-flash-tiered", _THINKING_LEVEL_HIGH),
    "claude-opus-4.6": ("claude-opus-4-6-thinking", _THINKING_BUDGET_DEFAULT),
    "claude-opus-4-6": ("claude-opus-4-6-thinking", _THINKING_BUDGET_DEFAULT),
    "claude-opus-4-6-thinking": ("claude-opus-4-6-thinking", _THINKING_BUDGET_DEFAULT),
    "claude-sonnet-4.6": ("claude-sonnet-4-6", _THINKING_BUDGET_DEFAULT),
    "claude-sonnet-4-6": ("claude-sonnet-4-6", _THINKING_BUDGET_DEFAULT),
    "gemini-3.6-flash": ("gemini-3.6-flash-high", _THINKING_BUDGET_DEFAULT),
    "gemini-3.6-flash-high": ("gemini-3.6-flash-high", _THINKING_BUDGET_DEFAULT),
    "gemini-3.1-pro": ("gemini-pro-agent", _THINKING_BUDGET_DEFAULT),
    "gemini-pro-agent": ("gemini-pro-agent", _THINKING_BUDGET_DEFAULT),
    "gpt-oss-120b": ("gpt-oss-120b-medium", _THINKING_BUDGET_DEFAULT),
    "gpt-oss-120b-medium": ("gpt-oss-120b-medium", _THINKING_BUDGET_DEFAULT),
}


def resolve_antigravity_project_id() -> str:
    env_proj = os.environ.get("ANTIGRAVITY_PROJECT_ID", "").strip()
    if env_proj:
        return env_proj

    auth_candidates = [
        Path.home() / ".senpi" / "agent" / "auth.json",
        Path("/root/.senpi/agent/auth.json"),
    ]
    for p in auth_candidates:
        if p.exists():
            try:
                data = json.loads(p.read_text(encoding="utf-8"))
                proj = data.get("antigravity", {}).get("projectId")
                if isinstance(proj, str) and proj.strip():
                    return proj.strip()
            except (OSError, json.JSONDecodeError):
                pass
    return "lithe-dogfish-7dc4d"


def get_runtime_model_and_thinking_config(
    model_id: str,
) -> tuple[str, dict[str, Any]]:
    m = model_id.lower()
    if m in _STATIC_ROUTING_MAP:
        return _STATIC_ROUTING_MAP[m]
    return model_id, _THINKING_BUDGET_DEFAULT


class AntigravityAPIError(Exception):
    """Exception raised for errors returned by the Antigravity API."""

    def __init__(self, status_code: int, message: str) -> None:
        super().__init__(f"Antigravity API error {status_code}: {message}")
        self.status_code = status_code
        self.message = message


class AntigravityProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="Google Antigravity",
            source="antigravity",
            api_key_env_var="ANTIGRAVITY_API_KEY",
            message_protocol="openai",
            default_reasoning_effort="high",
            default_thinking_budget=32000,
        )

    def map_litellm_kwargs(
        self, model: SleepyRouterModel, api_key: str, kwargs: dict[str, Any]
    ) -> dict[str, Any]:
        upstream_id = model.upstream_id or model.id
        runtime_id, _ = get_runtime_model_and_thinking_config(upstream_id)
        res = inject_max_reasoning(
            kwargs,
            effort="high",
            thinking_budget=32000,
        )
        res["model"] = f"openai/{runtime_id}"
        res["api_base"] = ANTIGRAVITY_BASE_URL
        res["api_key"] = api_key
        res["headers"] = {
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
        return res


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
