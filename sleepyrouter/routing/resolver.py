"""Candidate model resolution."""

from typing import Any

from .groups import normalize_model_group_name, resolve_default_group

RouteReason = str  # "model-group" | "direct-model" | "fallback-order"


def candidate_ids(
    groups: dict[str, list[str]],
    requested_model: str,
    default_group: str | None,
    *group_order: str,
    known_models: dict[str, Any] | None = None,
) -> tuple[list[str], RouteReason]:
    normalized = normalize_model_group_name(requested_model)

    # 1. Match model group
    for g_k, g_v in groups.items():
        if normalize_model_group_name(g_k) == normalized:
            return g_v, "model-group"

    # 2. Match direct model ID
    if known_models:
        if requested_model in known_models:
            return [requested_model], "direct-model"
        for m_id in known_models:
            if normalize_model_group_name(m_id) == normalized:
                return [m_id], "direct-model"

    for g_vals in groups.values():
        for val in g_vals:
            if normalize_model_group_name(val) == normalized:
                return [val], "direct-model"

    # 3. Fallback to default group
    resolved = resolve_default_group(groups, default_group, *group_order)
    if not resolved:
        return [], "fallback-order"
    return groups.get(resolved, []), "fallback-order"


def ordered_candidates(
    groups: dict[str, list[str]],
    requested_model: str,
    default_group: str | None,
    *group_order: str,
    known_models: dict[str, Any] | None = None,
) -> tuple[list[str], RouteReason]:
    ids, reason = candidate_ids(
        groups, requested_model, default_group, *group_order, known_models=known_models
    )
    return list(ids), reason
