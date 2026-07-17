package routing

import (
	"fmt"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

type RouteReason string

const (
	RouteModelGroup    RouteReason = "model-group"
	RouteFallbackOrder RouteReason = "fallback-order"
)

type RouteChoice struct {
	ModelID string      `json:"modelId"`
	Reason  RouteReason `json:"reason"`
}

func CandidateIDs(groups types.ModelGroups, requestedModel, defaultGroup string, groupOrder ...string) []string {
	normalized := NormalizeModelGroupName(requestedModel)
	if normalized != "" {
		if ids, ok := groups[normalized]; ok {
			return ids
		}
	}
	resolved := ResolveDefaultGroup(groups, defaultGroup, groupOrder...)
	if resolved == "" {
		return []string{}
	}
	return groups[resolved]
}

func OrderedCandidates(groups types.ModelGroups, requestedModel, defaultGroup string, groupOrder ...string) []string {
	return append([]string(nil), CandidateIDs(groups, requestedModel, defaultGroup, groupOrder...)...)
}

func ChooseModel(groups types.ModelGroups, requestedModel string, groupOrder ...string) (RouteChoice, error) {
	ids := CandidateIDs(groups, requestedModel, "", groupOrder...)
	if len(ids) == 0 {
		return RouteChoice{}, fmt.Errorf("선택된 모델이 없어요. config.json의 modelGroups에 모델을 하나 이상 추가하세요. (요청: %s, 사용 가능한 그룹: %s)", displayRequestedModel(requestedModel), groupNames(groups, groupOrder...))
	}
	normalized := NormalizeModelGroupName(requestedModel)
	reason := RouteFallbackOrder
	if normalized != "" {
		if _, ok := groups[normalized]; ok {
			reason = RouteModelGroup
		}
	}
	return RouteChoice{ModelID: ids[0], Reason: reason}, nil
}

func ChooseGroupedModel(groups types.ModelGroups, requestedModel, defaultGroup string, groupOrder ...string) (RouteChoice, error) {
	ids := CandidateIDs(groups, requestedModel, defaultGroup, groupOrder...)
	if len(ids) == 0 {
		return RouteChoice{}, fmt.Errorf("선택된 모델이 없어요. config.json의 modelGroups에 모델을 하나 이상 추가하세요. (요청: %s, 기본그룹: %s, 사용 가능한 그룹: %s)", displayRequestedModel(requestedModel), displayRequestedModel(defaultGroup), groupNames(groups, groupOrder...))
	}
	normalized := NormalizeModelGroupName(requestedModel)
	reason := RouteFallbackOrder
	if normalized != "" {
		if _, ok := groups[normalized]; ok {
			reason = RouteModelGroup
		}
	}
	return RouteChoice{ModelID: ids[0], Reason: reason}, nil
}

func displayRequestedModel(value string) string {
	if value == "" {
		return "없음"
	}
	return value
}

func groupNames(groups types.ModelGroups, groupOrder ...string) string {
	order := types.CompleteGroupOrder(groups, groupOrder)
	if len(order) == 0 {
		return "없음"
	}
	result := ""
	for index, name := range order {
		if index > 0 {
			result += ", "
		}
		result += name
	}
	return result
}
