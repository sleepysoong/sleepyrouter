"""Candidate model resolution."""

from .groups import normalize_model_group_name, resolve_default_group

RouteReason = str  # "model-group" | "fallback-order"


def candidate_ids(
    groups: dict[str, list[str]],
    requested_model: str,
    default_group: str | None,
    *group_order: str,
) -> tuple[list[str], RouteReason]:
    normalized = normalize_model_group_name(requested_model)
    if normalized and normalized in groups:
        return groups[normalized], "model-group"
    resolved = resolve_default_group(groups, default_group, *group_order)
    if not resolved:
        return [], "fallback-order"
    return groups.get(resolved, []), "fallback-order"


def ordered_candidates(
    groups: dict[str, list[str]],
    requested_model: str,
    default_group: str | None,
    *group_order: str,
) -> tuple[list[str], RouteReason]:
    ids, reason = candidate_ids(groups, requested_model, default_group, *group_order)
    return list(ids), reason
