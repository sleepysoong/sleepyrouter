"""Routing logic for model groups and candidates."""

from typing import Any

from .types import complete_group_order

RouteReason = str  # "model-group" | "fallback-order"


def normalize_model_group_name(value: str) -> str:
    if not value:
        return ""
    return value.strip().lower()


def normalize_model_groups_ordered(
    value: Any,
) -> tuple[dict[str, list[str]], list[str]]:
    groups: dict[str, list[str]] = {}
    order: list[str] = []

    if isinstance(value, dict):
        for key, raw in value.items():
            if isinstance(raw, list):
                ids = [str(v) for v in raw if isinstance(v, (str, int))]
                groups[key] = ids
                order.append(key)
    order.sort()
    return groups, order


def all_group_model_ids(groups: dict[str, list[str]], *group_order: str) -> list[str]:
    seen = set()
    result: list[str] = []
    for group in complete_group_order(groups, list(group_order)):
        for model_id in groups.get(group, []):
            if model_id not in seen:
                seen.add(model_id)
                result.append(model_id)
    return result


def resolve_default_group(
    groups: dict[str, list[str]], default_group: str | None, *group_order: str
) -> str:
    if default_group and default_group in groups:
        return default_group
    order = complete_group_order(groups, list(group_order))
    return order[0] if order else ""


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
