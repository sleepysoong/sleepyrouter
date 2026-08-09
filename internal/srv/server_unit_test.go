package srv

import (
	"strings"
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/handler"
	"github.com/sleepysoong/sleepyrouter/internal/httperr"
	"github.com/sleepysoong/sleepyrouter/internal/types"
)

func TestModelUpstreamID_MultiSlash(t *testing.T) {
	// NVIDIA: "nvidia/b/c" → upstream "b/c"
	nvidiaModel := types.SleepyRouterModel{ID: "nvidia/b/c", UpstreamID: "b/c", Provider: "nvidia", Source: types.SourceNVIDIA}
	if got := handler.ModelUpstreamID(nvidiaModel); got != "b/c" {
		t.Fatalf("nvidia ModelUpstreamID: got %q, want b/c", got)
	}

	// OpenRouter: uses UpstreamID if present
	orModel := types.SleepyRouterModel{ID: "openrouter/b/c", UpstreamID: "b/c", Provider: "openrouter", Source: types.SourceOpenRouter}
	if got := handler.ModelUpstreamID(orModel); got != "b/c" {
		t.Fatalf("openrouter ModelUpstreamID: got %q, want b/c", got)
	}

	// Copilot: "copilot/b/c" → upstream "b/c"
	copilotModel := types.SleepyRouterModel{ID: "copilot/b/c", UpstreamID: "b/c", Provider: "copilot", Source: types.SourceCopilot}
	if got := handler.ModelUpstreamID(copilotModel); got != "b/c" {
		t.Fatalf("copilot ModelUpstreamID: got %q, want b/c", got)
	}
}

func TestSafeLogValue(t *testing.T) {
	if got := httperr.SafeLogValue("hello"); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if got := httperr.SafeLogValue(strings.Repeat("x", 300)); len(got) > 203 {
		t.Fatalf("too long: %d", len(got))
	}
}
