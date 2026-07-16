package sleepyrouter

import (
	"testing"
)

func TestNormalizeModelGroupName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"fast", "fast"},
		{"sleepyrouter/fast", "fast"},
		{"SLEEPYROUTER/Fast", "fast"},
		{"haiku", "fast"},
		{"sonnet", "balanced"},
		{"opus", "capable"},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range tests {
		got := NormalizeModelGroupName(tc.input)
		if got != tc.expected {
			t.Errorf("NormalizeModelGroupName(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestNormalizeModelGroups(t *testing.T) {
	groups := NormalizeModelGroups(map[string]any{
		"a": []any{"x", "y"},
		"b": []any{42, "z"},
	})
	if g, ok := groups["a"]; !ok || len(g) != 2 || g[0] != "x" {
		t.Fatalf("groups[a]: %v", g)
	}
	if len(groups["b"]) != 1 || groups["b"][0] != "z" {
		t.Fatalf("groups[b]: %v", groups["b"])
	}
}

func TestAllGroupModelIDs_Deduplicates(t *testing.T) {
	groups := ModelGroups{"a": {"x", "y"}, "b": {"y", "z"}}
	ids := AllGroupModelIDs(groups)
	if len(ids) != 3 {
		t.Fatalf("got %d ids: %v", len(ids), ids)
	}
}

func TestSelectedGroupModelIDs_Found(t *testing.T) {
	groups := ModelGroups{"coding": {"a", "b"}}
	ids := SelectedGroupModelIDs(groups, "coding")
	if len(ids) != 2 || ids[0] != "a" {
		t.Fatalf("got %v", ids)
	}
}

func TestSelectedGroupModelIDs_NotFound(t *testing.T) {
	groups := ModelGroups{"coding": {"a"}}
	ids := SelectedGroupModelIDs(groups, "unknown")
	if ids != nil {
		t.Fatalf("expected nil, got %v", ids)
	}
}

func TestSelectedGroupModelIDs_EmptyGroup(t *testing.T) {
	groups := ModelGroups{"coding": {}}
	ids := SelectedGroupModelIDs(groups, "coding")
	if ids != nil {
		t.Fatalf("expected nil, got %v", ids)
	}
}

func TestResolveDefaultGroup(t *testing.T) {
	groups := ModelGroups{"fast": {"a"}, "balanced": {"b"}}
	if got := ResolveDefaultGroup(groups, ""); got != "balanced" {
		t.Fatalf("expected balanced, got %s", got)
	}
}

func TestResolveDefaultGroup_Explicit(t *testing.T) {
	groups := ModelGroups{"fast": {"a"}, "balanced": {"b"}}
	if got := ResolveDefaultGroup(groups, "fast"); got != "fast" {
		t.Fatalf("expected fast, got %s", got)
	}
}
