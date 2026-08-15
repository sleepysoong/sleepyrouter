import type { ModelGroups } from "../types.js";
import { candidateIDs, type RouteReason } from "./candidate-resolver.js";

export interface RoutingStrategy {
  resolveCandidates(
    groups: ModelGroups,
    requestedModel: string,
    defaultGroup?: string,
    groupOrder?: string[],
  ): { ids: string[]; reason: RouteReason };
}

export class GroupFallbackRoutingStrategy implements RoutingStrategy {
  resolveCandidates(
    groups: ModelGroups,
    requestedModel: string,
    defaultGroup?: string,
    groupOrder: string[] = [],
  ): { ids: string[]; reason: RouteReason } {
    return candidateIDs(groups, requestedModel, defaultGroup, ...groupOrder);
  }
}

export class RoutingEngine {
  constructor(private strategy: RoutingStrategy = new GroupFallbackRoutingStrategy()) {}

  setStrategy(strategy: RoutingStrategy): void {
    this.strategy = strategy;
  }

  resolve(
    groups: ModelGroups,
    requestedModel: string,
    defaultGroup?: string,
    groupOrder?: string[],
  ): { ids: string[]; reason: RouteReason } {
    return this.strategy.resolveCandidates(
      groups,
      requestedModel,
      defaultGroup,
      groupOrder,
    );
  }
}

export const defaultRoutingEngine = new RoutingEngine();
