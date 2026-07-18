package cli

import (
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

func TestUndefinedModelAliases_AllDefined(t *testing.T) {
	groups := types.ModelGroups{
		"main": {"copilot/gpt-4o", "copilot/claude-sonnet-4"},
	}
	models := map[string]types.ModelDefinition{
		"copilot/gpt-4o":          {Provider: "copilot", Name: "gpt-4o"},
		"copilot/claude-sonnet-4": {Provider: "copilot", Name: "claude-sonnet-4"},
	}
	if got := undefinedModelAliases(groups, models); len(got) != 0 {
		t.Fatalf("expected no undefined aliases, got %v", got)
	}
}

func TestUndefinedModelAliases_NvidiaAndOpenRouterDefined(t *testing.T) {
	groups := types.ModelGroups{
		"a": {"nvidia/meta/llama-3.1-70b"},
		"b": {"openrouter/openai/gpt-4o"},
	}
	models := map[string]types.ModelDefinition{
		"nvidia/meta/llama-3.1-70b": {Provider: "nvidia", Name: "meta/llama-3.1-70b"},
		"openrouter/openai/gpt-4o":  {Provider: "openrouter", Name: "openai/gpt-4o"},
	}
	if got := undefinedModelAliases(groups, models); len(got) != 0 {
		t.Fatalf("expected no undefined aliases, got %v", got)
	}
}

func TestUndefinedModelAliases_MissingEntriesDetected(t *testing.T) {
	groups := types.ModelGroups{
		"zeta":  {"gpt-4o"},
		"alpha": {"foo/bar", "nvidia/ok-model"},
	}
	models := map[string]types.ModelDefinition{
		"nvidia/ok-model": {Provider: "nvidia", Name: "ok-model"},
	}
	got := undefinedModelAliases(groups, models)
	if len(got) != 2 {
		t.Fatalf("expected 2 undefined aliases, got %v", got)
	}
	// groups iterated in sorted order: alpha before zeta
	if got[0] != "alpha: foo/bar" {
		t.Fatalf("got[0] = %q, want %q", got[0], "alpha: foo/bar")
	}
	if got[1] != "zeta: gpt-4o" {
		t.Fatalf("got[1] = %q, want %q", got[1], "zeta: gpt-4o")
	}
}
