package routing_test

import (
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
)

func testSnapshot() *config.RuntimeSnapshot {
	cfg, _ := config.Parse([]byte(`
version = 1
[routing]
default_group = "coding"
[models."zen/a"]
provider = "zen"
upstream_model = "a"
[models."nvidia/b"]
provider = "nvidia"
upstream_model = "b"
[models."openrouter/c"]
provider = "openrouter"
upstream_model = "c"
[models."gemini/novision"]
provider = "gemini"
upstream_model = "nv"
[models."gemini/novision".capabilities]
vision = false
[models."zen/disabled"]
provider = "zen"
upstream_model = "d"
enabled = false
[groups]
coding = ["zen/a", "nvidia/b", "openrouter/c"]
fast = ["nvidia/b"]
[aliases]
"claude-sleepy" = "coding"
"single" = "zen/a"
`))
	return config.BuildSnapshot(cfg, map[string]string{
		"OPENCODE_API_KEY": "x", "NVIDIA_API_KEY": "x", "OPENROUTER_API_KEY": "x", "GEMINI_API_KEY": "x",
	}, 1)
}

func TestGroupExact(t *testing.T) {
	ids, reason, err := routing.Resolve(testSnapshot(), "coding")
	if err != nil || reason != routing.ReasonGroup || len(ids) != 3 || ids[0] != "zen/a" {
		t.Fatalf("got %v %v %v", ids, reason, err)
	}
}

func TestDirectModel(t *testing.T) {
	ids, reason, err := routing.Resolve(testSnapshot(), "nvidia/b")
	if err != nil || reason != routing.ReasonDirect || len(ids) != 1 {
		t.Fatalf("got %v %v %v", ids, reason, err)
	}
}

func TestAliasGroup(t *testing.T) {
	ids, reason, err := routing.Resolve(testSnapshot(), "claude-sleepy")
	if err != nil || reason != routing.ReasonAlias || len(ids) != 3 {
		t.Fatalf("got %v %v %v", ids, reason, err)
	}
}

func TestAliasModel(t *testing.T) {
	ids, _, err := routing.Resolve(testSnapshot(), "single")
	if err != nil || len(ids) != 1 || ids[0] != "zen/a" {
		t.Fatalf("got %v %v", ids, err)
	}
}

func TestDefaultFallback(t *testing.T) {
	ids, reason, err := routing.Resolve(testSnapshot(), "gpt-unknown-xyz")
	if err != nil || reason != routing.ReasonDefault || len(ids) != 3 {
		t.Fatalf("got %v %v %v", ids, reason, err)
	}
}

func TestUnknownErrorPolicy(t *testing.T) {
	snap := testSnapshot()
	snap.Routing.UnknownModelPolicy = "error"
	if _, _, err := routing.Resolve(snap, "nope"); err == nil {
		t.Fatal("expected unknown model error")
	}
}

func TestCapabilitySkip(t *testing.T) {
	snap := testSnapshot()
	// Put novision model in a group and require vision.
	snap.Groups["v"] = []string{"gemini/novision", "zen/a"}
	cands := routing.FilterCandidates(snap, []string{"gemini/novision", "zen/a"}, routing.Requirements{Vision: true})
	if len(cands) != 1 || cands[0].LocalModelID != "zen/a" {
		t.Fatalf("got %+v", cands)
	}
}

func TestDisabledSkip(t *testing.T) {
	snap := testSnapshot()
	cands := routing.FilterCandidates(snap, []string{"zen/disabled", "zen/a"}, routing.Requirements{})
	if len(cands) != 1 || cands[0].LocalModelID != "zen/a" {
		t.Fatalf("got %+v", cands)
	}
}

func TestOrderPreserved(t *testing.T) {
	snap := testSnapshot()
	cands, _, _ := routing.ResolveCandidates(snap, routing.RouteRequest{RequestedModel: "coding"})
	if len(cands) != 3 || cands[0].LocalModelID != "zen/a" || cands[1].LocalModelID != "nvidia/b" || cands[2].LocalModelID != "openrouter/c" {
		t.Fatalf("order broken: %+v", cands)
	}
}

func TestClassify(t *testing.T) {
	if routing.ClassifyStatus(429) != routing.ErrorRateLimit {
		t.Fatal("429")
	}
	if routing.ClassifyStatus(503) != routing.ErrorUpstream {
		t.Fatal("503")
	}
	if got := routing.ClassifyBody(400, "", "", "unsupported parameter: reasoning"); got != routing.ErrorUnsupportedFeature {
		t.Fatalf("400 unsupported = %v", got)
	}
	if got := routing.ClassifyBody(400, "", "", "you messed up json"); got != routing.ErrorClient {
		t.Fatalf("400 client = %v", got)
	}
	if !routing.ErrorUpstream.Failoverable() || routing.ErrorClient.Failoverable() {
		t.Fatal("failoverable")
	}
}
