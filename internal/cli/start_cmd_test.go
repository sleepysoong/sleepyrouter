package cli

import (
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

func TestInvalidModelIDs_CopilotAccepted(t *testing.T) {
	groups := types.ModelGroups{
		"main": {"copilot/gpt-4o", "copilot/claude-sonnet-4"},
	}
	if got := invalidModelIDs(groups); len(got) != 0 {
		t.Fatalf("expected no invalid IDs, got %v", got)
	}
}

func TestInvalidModelIDs_NvidiaAndOpenRouterAccepted(t *testing.T) {
	groups := types.ModelGroups{
		"a": {"nvidia/meta/llama-3.1-70b"},
		"b": {"openrouter/openai/gpt-4o"},
	}
	if got := invalidModelIDs(groups); len(got) != 0 {
		t.Fatalf("expected no invalid IDs, got %v", got)
	}
}

func TestInvalidModelIDs_UnknownPrefixRejected(t *testing.T) {
	groups := types.ModelGroups{
		"zeta":  {"gpt-4o"},
		"alpha": {"foo/bar", "nvidia/ok-model"},
	}
	got := invalidModelIDs(groups)
	if len(got) != 2 {
		t.Fatalf("expected 2 invalid IDs, got %v", got)
	}
	// groups iterated in sorted order: alpha before zeta
	if got[0] != "alpha: foo/bar" {
		t.Fatalf("got[0] = %q, want %q", got[0], "alpha: foo/bar")
	}
	if got[1] != "zeta: gpt-4o" {
		t.Fatalf("got[1] = %q, want %q", got[1], "zeta: gpt-4o")
	}
}
