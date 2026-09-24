package server

import (
	"net/http"
	"sort"
)

// handleModels returns configured groups and models for discovery.
// Claude Code currently filters discovered model IDs to those containing
// "claude" or "anthropic"; the gateway returns all IDs and lets clients filter.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	snap := s.deps.Store.Current()
	if snap == nil {
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": []any{}})
		return
	}
	type item struct {
		ID          string `json:"id"`
		Object      string `json:"object"`
		Type        string `json:"type"`
		Created     int64  `json:"created"`
		OwnedBy     string `json:"owned_by"`
		DisplayName string `json:"display_name"`
	}
	var data []item
	seen := map[string]bool{}
	add := func(id, owner string) {
		if seen[id] {
			return
		}
		seen[id] = true
		data = append(data, item{ID: id, Object: "model", Type: "model", OwnedBy: owner, DisplayName: id})
	}
	// Groups first (ordered), then models.
	for _, g := range snap.GroupOrder {
		add(g, "sleepyrouter")
	}
	var groupKeys []string
	for g := range snap.Groups {
		found := false
		for _, o := range snap.GroupOrder {
			if o == g {
				found = true
				break
			}
		}
		if !found {
			groupKeys = append(groupKeys, g)
		}
	}
	sort.Strings(groupKeys)
	for _, g := range groupKeys {
		add(g, "sleepyrouter")
	}
	var modelIDs []string
	for id := range snap.Models {
		modelIDs = append(modelIDs, id)
	}
	sort.Strings(modelIDs)
	for _, id := range modelIDs {
		m := snap.Models[id]
		add(id, m.ProviderID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}
