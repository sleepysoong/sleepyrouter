"""Pluggable RoutingStrategy pattern."""

from typing import Protocol

from .resolver import RouteReason, candidate_ids


class RoutingStrategy(Protocol):
    def resolve_candidates(
        self,
        groups: dict[str, list[str]],
        requested_model: str,
        default_group: str | None = None,
        group_order: list[str] | None = None,
    ) -> tuple[list[str], RouteReason]: ...


class GroupFallbackRoutingStrategy(RoutingStrategy):
    def resolve_candidates(
        self,
        groups: dict[str, list[str]],
        requested_model: str,
        default_group: str | None = None,
        group_order: list[str] | None = None,
    ) -> tuple[list[str], RouteReason]:
        order = group_order or []
        return candidate_ids(groups, requested_model, default_group, *order)


class RoutingEngine:
    def __init__(self, strategy: RoutingStrategy | None = None):
        self.strategy = strategy or GroupFallbackRoutingStrategy()

    def set_strategy(self, strategy: RoutingStrategy) -> None:
        self.strategy = strategy

    def resolve(
        self,
        groups: dict[str, list[str]],
        requested_model: str,
        default_group: str | None = None,
        group_order: list[str] | None = None,
    ) -> tuple[list[str], RouteReason]:
        return self.strategy.resolve_candidates(
            groups, requested_model, default_group, group_order
        )


default_routing_engine = RoutingEngine()
