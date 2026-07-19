package routing

import (
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

// Tests mirrored from test/router.test.ts

func TestRouter_RoutesToGroup(t *testing.T) {
	groups := types.ModelGroups{"fast": {"a", "b"}, "balanced": {"c"}}
	ids, reason := OrderedCandidates(groups, "fast", "")
	if len(ids) == 0 || ids[0] != "a" || reason != RouteModelGroup {
		t.Fatalf("ids: %v, reason: %v", ids, reason)
	}
}

func TestRouter_FallbackToDefaultGroup(t *testing.T) {
	groups := types.ModelGroups{"fast": {"a", "b"}, "balanced": {"c"}}
	ids, reason := OrderedCandidates(groups, "auto", "", "fast", "balanced")
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" || reason != RouteFallbackOrder {
		t.Fatalf("ids: %v, reason: %v", ids, reason)
	}
}

func TestRouter_NoRequestFallsBack(t *testing.T) {
	groups := types.ModelGroups{"default": {"z", "a"}}
	ids, reason := OrderedCandidates(groups, "", "")
	if len(ids) == 0 || ids[0] != "z" || reason != RouteFallbackOrder {
		t.Fatalf("ids: %v, reason: %v", ids, reason)
	}
}

func TestRouter_GroupOrderAsCandidates(t *testing.T) {
	groups := types.ModelGroups{"default": {"a", "b", "c"}}
	ids, reason := OrderedCandidates(groups, "", "")
	if len(ids) != 3 || ids[0] != "a" || ids[1] != "b" || ids[2] != "c" || reason != RouteFallbackOrder {
		t.Fatalf("ids: %v, reason: %v", ids, reason)
	}
}

func TestRouter_GroupAliases(t *testing.T) {
	groups := types.ModelGroups{"fast": {"b"}, "balanced": {"a"}, "capable": {"c"}}
	ids, reason := OrderedCandidates(groups, "fast", "")
	if len(ids) == 0 || ids[0] != "b" || reason != RouteModelGroup {
		t.Fatalf("ids: %v, reason: %v", ids, reason)
	}
}

func TestRouter_EmptyGroups(t *testing.T) {
	ids, _ := OrderedCandidates(types.ModelGroups{"fast": {}, "balanced": {}, "capable": {}}, "unknown", "")
	if len(ids) != 0 {
		t.Fatalf("expected empty, got %v", ids)
	}
}

func TestRouter_CustomGroupByName(t *testing.T) {
	groups := types.ModelGroups{"coding": {"model-a", "model-b"}, "chat": {"model-c"}}
	ids, reason := OrderedCandidates(groups, "coding", "")
	if len(ids) == 0 || ids[0] != "model-a" || reason != RouteModelGroup {
		t.Fatalf("ids: %v, reason: %v", ids, reason)
	}
}

func TestRouter_AllModelsInGroupAsCandidates(t *testing.T) {
	groups := types.ModelGroups{"coding": {"model-a", "model-b", "model-c"}}
	ids, reason := OrderedCandidates(groups, "coding", "")
	if len(ids) != 3 || ids[0] != "model-a" || ids[1] != "model-b" || ids[2] != "model-c" || reason != RouteModelGroup {
		t.Fatalf("ids: %v, reason: %v", ids, reason)
	}
}

func TestRouter_FallbackToDefaultGroupWhenNotAGroup(t *testing.T) {
	groups := types.ModelGroups{"coding": {"model-a", "model-b"}, "default": {"model-c", "model-d"}}
	ids, reason := OrderedCandidates(groups, "unknown-model", "default")
	if len(ids) != 2 || ids[0] != "model-c" || ids[1] != "model-d" || reason != RouteFallbackOrder {
		t.Fatalf("ids: %v, reason: %v", ids, reason)
	}
}

func TestRouter_FallbackToFirstGroupWhenDefaultNotSet(t *testing.T) {
	groups := types.ModelGroups{"coding": {"model-a", "model-b"}, "chat": {"model-c"}}
	ids, reason := OrderedCandidates(groups, "unknown-model", "", "coding", "chat")
	if len(ids) != 2 || ids[0] != "model-a" || ids[1] != "model-b" || reason != RouteFallbackOrder {
		t.Fatalf("ids: %v, reason: %v", ids, reason)
	}
}

func TestRouter_DefaultGroupForAuto(t *testing.T) {
	groups := types.ModelGroups{"fast": {"model-a"}, "default": {"model-b", "model-c"}}
	ids, reason := OrderedCandidates(groups, "auto", "default")
	if len(ids) != 2 || ids[0] != "model-b" || ids[1] != "model-c" || reason != RouteFallbackOrder {
		t.Fatalf("ids: %v, reason: %v", ids, reason)
	}
}

func TestRouter_AllGroupModelIDs(t *testing.T) {
	groups := types.ModelGroups{"a": {"x", "y"}, "b": {"y", "z"}}
	ids := AllGroupModelIDs(groups)
	if len(ids) != 3 || ids[0] != "x" || ids[1] != "y" || ids[2] != "z" {
		t.Fatalf("all group ids: %v", ids)
	}
}

func TestRouter_SelectedGroupModelIDsUnknown(t *testing.T) {
	groups := types.ModelGroups{"coding": {"model-a"}}
	ids := selectedGroupModelIDs(groups, "unknown")
	if ids != nil {
		t.Fatalf("expected nil, got %v", ids)
	}
}
