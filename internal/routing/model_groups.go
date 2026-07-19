package routing

import (
	"sort"
	"strings"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

func NormalizeModelGroupName(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func NormalizeModelGroups(value any) types.ModelGroups {
	groups, _ := NormalizeModelGroupsOrdered(value)
	return groups
}

func NormalizeModelGroupsOrdered(value any) (types.ModelGroups, []string) {
	groups := types.ModelGroups{}
	order := []string{}
	switch source := value.(type) {
	case map[string]any:
		for key, raw := range source {
			ids := stringsFromUnknownSlice(raw)
			if ids == nil {
				continue
			}
			groups[key] = ids
			order = append(order, key)
		}
	case map[string][]string:
		for key, ids := range source {
			groups[key] = append([]string(nil), ids...)
			order = append(order, key)
		}
	}
	// JSON decoding into map loses order. ReadConfig supplies its own order below;
	// direct map callers get deterministic lexical ordering instead of random routing.
	sort.Strings(order)
	return groups, order
}

func stringsFromUnknownSlice(value any) []string {
	switch values := value.(type) {
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				out = append(out, text)
			}
		}
		return out
	case []string:
		return append([]string(nil), values...)
	default:
		return nil
	}
}

func AllGroupModelIDs(groups types.ModelGroups, groupOrder ...string) []string {
	seen := make(map[string]bool)
	result := []string{}
	for _, group := range types.CompleteGroupOrder(groups, groupOrder) {
		for _, id := range groups[group] {
			if !seen[id] {
				seen[id] = true
				result = append(result, id)
			}
		}
	}
	return result
}

func SelectedGroupModelIDs(groups types.ModelGroups, requestedModel string) []string {
	group := NormalizeModelGroupName(requestedModel)
	ids, ok := groups[group]
	if group == "" || !ok || len(ids) == 0 {
		return nil
	}
	return ids
}

func ResolveDefaultGroup(groups types.ModelGroups, defaultGroup string, groupOrder ...string) string {
	if defaultGroup != "" {
		if _, ok := groups[defaultGroup]; ok {
			return defaultGroup
		}
	}
	order := types.CompleteGroupOrder(groups, groupOrder)
	if len(order) == 0 {
		return ""
	}
	return order[0]
}
