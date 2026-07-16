package sleepyrouter

import (
	"sort"
	"strings"
)

var legacyAliases = map[string]string{
	"haiku":  "fast",
	"sonnet": "balanced",
	"opus":   "capable",
}

func NormalizeModelGroupName(value string) string {
	if value == "" {
		return ""
	}
	normalized := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "slr/")
	if alias, ok := legacyAliases[normalized]; ok {
		return alias
	}
	return normalized
}

func NormalizeModelGroups(value any) ModelGroups {
	groups, _ := NormalizeModelGroupsOrdered(value)
	return groups
}

func NormalizeModelGroupsOrdered(value any) (ModelGroups, []string) {
	groups := ModelGroups{}
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

func completeGroupOrder(groups ModelGroups, preferred []string) []string {
	seen := make(map[string]bool)
	order := make([]string, 0, len(groups))
	for _, name := range preferred {
		if !seen[name] {
			if _, ok := groups[name]; ok {
				seen[name] = true
				order = append(order, name)
			}
		}
	}
	remaining := make([]string, 0, len(groups)-len(order))
	for name := range groups {
		if !seen[name] {
			remaining = append(remaining, name)
		}
	}
	sort.Strings(remaining)
	return append(order, remaining...)
}

func AllGroupModelIDs(groups ModelGroups, groupOrder ...string) []string {
	seen := make(map[string]bool)
	result := []string{}
	for _, group := range completeGroupOrder(groups, groupOrder) {
		for _, id := range groups[group] {
			if !seen[id] {
				seen[id] = true
				result = append(result, id)
			}
		}
	}
	return result
}

func SelectedGroupModelIDs(groups ModelGroups, requestedModel string) []string {
	group := NormalizeModelGroupName(requestedModel)
	ids, ok := groups[group]
	if group == "" || !ok || len(ids) == 0 {
		return nil
	}
	return ids
}

func ResolveDefaultGroup(groups ModelGroups, defaultGroup string, groupOrder ...string) string {
	if defaultGroup != "" {
		if _, ok := groups[defaultGroup]; ok {
			return defaultGroup
		}
	}
	order := completeGroupOrder(groups, groupOrder)
	if len(order) == 0 {
		return ""
	}
	return order[0]
}
