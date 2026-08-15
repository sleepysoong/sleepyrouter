"""Model group normalization and ordering."""

from typing import Any

from sleepyrouter.types import complete_group_order


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
