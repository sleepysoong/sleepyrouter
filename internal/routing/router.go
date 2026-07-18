package routing

import (
	"github.com/sleepysoong/sleepyrouter/internal/types"
)

type RouteReason string

const (
	RouteModelGroup    RouteReason = "model-group"
	RouteFallbackOrder RouteReason = "fallback-order"
)

func CandidateIDs(groups types.ModelGroups, requestedModel, defaultGroup string, groupOrder ...string) ([]string, RouteReason) {
	normalized := NormalizeModelGroupName(requestedModel)
	if normalized != "" {
		if ids, ok := groups[normalized]; ok {
			return ids, RouteModelGroup
		}
	}
	resolved := ResolveDefaultGroup(groups, defaultGroup, groupOrder...)
	if resolved == "" {
		return []string{}, RouteFallbackOrder
	}
	return groups[resolved], RouteFallbackOrder
}

func OrderedCandidates(groups types.ModelGroups, requestedModel, defaultGroup string, groupOrder ...string) ([]string, RouteReason) {
	ids, reason := CandidateIDs(groups, requestedModel, defaultGroup, groupOrder...)
	return append([]string(nil), ids...), reason
}
