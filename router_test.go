package sleepyrouter

import (
	"testing"
)

// Tests mirrored from test/router.test.ts

func TestRouter_RoutesToGroup(t *testing.T) {
	groups := ModelGroups{"fast": {"a", "b"}, "balanced": {"c"}}
	choice, err := ChooseModel(groups, "fast")
	if err != nil {
		t.Fatal(err)
	}
	if choice.ModelID != "a" || choice.Reason != RouteModelGroup {
		t.Fatalf("choice: %v", choice)
	}
}

func TestRouter_FallbackToDefaultGroup(t *testing.T) {
	groups := ModelGroups{"fast": {"a", "b"}, "balanced": {"c"}}
	ids := OrderedCandidates(groups, "auto", "", "fast", "balanced")
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("candidates: %v", ids)
	}
}

func TestRouter_NoRequestFallsBack(t *testing.T) {
	groups := ModelGroups{"default": {"z", "a"}}
	choice, err := ChooseModel(groups, "")
	if err != nil {
		t.Fatal(err)
	}
	if choice.ModelID != "z" || choice.Reason != RouteFallbackOrder {
		t.Fatalf("choice: %v", choice)
	}
}

func TestRouter_GroupOrderAsCandidates(t *testing.T) {
	groups := ModelGroups{"default": {"a", "b", "c"}}
	ids := OrderedCandidates(groups, "", "")
	if len(ids) != 3 || ids[0] != "a" || ids[1] != "b" || ids[2] != "c" {
		t.Fatalf("candidates: %v", ids)
	}
}

func TestRouter_GroupAliases(t *testing.T) {
	groups := ModelGroups{"fast": {"b"}, "balanced": {"a"}, "capable": {"c"}}
	choice, err := ChooseGroupedModel(groups, "sleepyrouter/fast", "")
	if err != nil {
		t.Fatal(err)
	}
	if choice.ModelID != "b" || choice.Reason != RouteModelGroup {
		t.Fatalf("choice: %v", choice)
	}
	ids := OrderedCandidates(groups, "haiku", "")
	if len(ids) != 1 || ids[0] != "b" {
		t.Fatalf("haiku candidates: %v", ids)
	}
}

func TestRouter_EmptyGroups(t *testing.T) {
	ids := OrderedCandidates(ModelGroups{"fast": {}, "balanced": {}, "capable": {}}, "opus", "")
	if len(ids) != 0 {
		t.Fatalf("expected empty, got %v", ids)
	}
}

func TestRouter_CustomGroupByName(t *testing.T) {
	groups := ModelGroups{"coding": {"model-a", "model-b"}, "chat": {"model-c"}}
	choice, err := ChooseGroupedModel(groups, "coding", "")
	if err != nil {
		t.Fatal(err)
	}
	if choice.ModelID != "model-a" || choice.Reason != RouteModelGroup {
		t.Fatalf("choice: %v", choice)
	}
}

func TestRouter_AllModelsInGroupAsCandidates(t *testing.T) {
	groups := ModelGroups{"coding": {"model-a", "model-b", "model-c"}}
	ids := OrderedCandidates(groups, "coding", "")
	if len(ids) != 3 || ids[0] != "model-a" || ids[1] != "model-b" || ids[2] != "model-c" {
		t.Fatalf("candidates: %v", ids)
	}
}

func TestRouter_FallbackToDefaultGroupWhenNotAGroup(t *testing.T) {
	groups := ModelGroups{"coding": {"model-a", "model-b"}, "default": {"model-c", "model-d"}}
	ids := OrderedCandidates(groups, "unknown-model", "default")
	if len(ids) != 2 || ids[0] != "model-c" || ids[1] != "model-d" {
		t.Fatalf("candidates: %v", ids)
	}
}

func TestRouter_FallbackToFirstGroupWhenDefaultNotSet(t *testing.T) {
	groups := ModelGroups{"coding": {"model-a", "model-b"}, "chat": {"model-c"}}
	ids := OrderedCandidates(groups, "unknown-model", "", "coding", "chat")
	if len(ids) != 2 || ids[0] != "model-a" || ids[1] != "model-b" {
		t.Fatalf("candidates: %v", ids)
	}
}

func TestRouter_DefaultGroupForAuto(t *testing.T) {
	groups := ModelGroups{"fast": {"model-a"}, "default": {"model-b", "model-c"}}
	ids := OrderedCandidates(groups, "auto", "default")
	if len(ids) != 2 || ids[0] != "model-b" || ids[1] != "model-c" {
		t.Fatalf("candidates: %v", ids)
	}
}

func TestRouter_IgnoreSleepyRouterPrefix(t *testing.T) {
	groups := ModelGroups{"coding": {"model-a", "model-b"}}
	choice, err := ChooseGroupedModel(groups, "sleepyrouter/coding", "")
	if err != nil {
		t.Fatal(err)
	}
	if choice.ModelID != "model-a" || choice.Reason != RouteModelGroup {
		t.Fatalf("choice: %v", choice)
	}
}

func TestRouter_LegacyAliases(t *testing.T) {
	groups := ModelGroups{"fast": {"model-a"}, "balanced": {"model-b"}, "capable": {"model-c"}}
	c1, err := ChooseGroupedModel(groups, "haiku", "")
	if err != nil {
		t.Fatal(err)
	}
	if c1.ModelID != "model-a" || c1.Reason != RouteModelGroup {
		t.Fatalf("haiku: %v", c1)
	}
	c2, err := ChooseGroupedModel(groups, "sonnet", "")
	if err != nil {
		t.Fatal(err)
	}
	if c2.ModelID != "model-b" || c2.Reason != RouteModelGroup {
		t.Fatalf("sonnet: %v", c2)
	}
	c3, err := ChooseGroupedModel(groups, "opus", "")
	if err != nil {
		t.Fatal(err)
	}
	if c3.ModelID != "model-c" || c3.Reason != RouteModelGroup {
		t.Fatalf("opus: %v", c3)
	}
}

func TestRouter_AllGroupModelIDs(t *testing.T) {
	groups := ModelGroups{"a": {"x", "y"}, "b": {"y", "z"}}
	ids := AllGroupModelIDs(groups)
	if len(ids) != 3 || ids[0] != "x" || ids[1] != "y" || ids[2] != "z" {
		t.Fatalf("all group ids: %v", ids)
	}
}

func TestRouter_SelectedGroupModelIDsUnknown(t *testing.T) {
	groups := ModelGroups{"coding": {"model-a"}}
	ids := SelectedGroupModelIDs(groups, "unknown")
	if ids != nil {
		t.Fatalf("expected nil, got %v", ids)
	}
}
