package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSourceOf(t *testing.T) {
	cases := []struct {
		source ModelSource
		want   ModelSource
	}{
		{SourceNVIDIA, SourceNVIDIA},
		{SourceCopilot, SourceCopilot},
		{SourceZen, SourceZen},
		{SourceOpenRouter, SourceOpenRouter},
		{SourceGoogle, SourceGoogle},
		{"", SourceOpenRouter},
		{"bogus", SourceOpenRouter},
	}
	for _, c := range cases {
		model := SleepyRouterModel{ID: "x/foo", Source: c.source}
		if got := SourceOf(model); got != c.want {
			t.Errorf("Source=%q: got %q, want %q", c.source, got, c.want)
		}
	}
}

func TestProviderAPIKeys_For(t *testing.T) {
	keys := ProviderAPIKeys{OpenRouter: "or", NVIDIA: "nv", Copilot: "co", Zen: "zn", Google: "gk"}
	cases := []struct {
		source ModelSource
		want   string
	}{
		{SourceNVIDIA, "nv"},
		{SourceCopilot, "co"},
		{SourceZen, "zn"},
		{SourceGoogle, "gk"},
		{SourceOpenRouter, "or"},
		{"unknown", "or"},
	}
	for _, c := range cases {
		if got := keys.For(c.source); got != c.want {
			t.Errorf("source=%q: got %q, want %q", c.source, got, c.want)
		}
	}
}

func TestCompleteGroupOrder(t *testing.T) {
	groups := ModelGroups{"alpha": {}, "beta": {}, "gamma": {}}

	// preferred order preserved; unknown names skipped; remaining alphabetical
	got := CompleteGroupOrder(groups, []string{"gamma", "missing", "alpha"})
	want := []string{"gamma", "alpha", "beta"}
	if len(got) != len(want) {
		t.Fatalf("len: got %d, want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i] != name {
			t.Fatalf("index %d: got %q, want %q", i, got[i], name)
		}
	}

	// empty preferred → full alphabetical
	got = CompleteGroupOrder(groups, nil)
	if len(got) != 3 || got[0] != "alpha" || got[1] != "beta" || got[2] != "gamma" {
		t.Fatalf("alphabetical order broken: %v", got)
	}
}

func TestSleepyRouterConfig_MarshalJSON(t *testing.T) {
	// nil groups should marshal as {} not null
	t.Run("nil groups becomes empty object", func(t *testing.T) {
		cfg := SleepyRouterConfig{Port: 4567}
		data, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		s := string(data)
		if !strings.Contains(s, `"modelGroups":{}`) {
			t.Errorf("expected empty modelGroups object, got: %s", s)
		}
		if !strings.Contains(s, `"port":4567`) {
			t.Errorf("expected port 4567, got: %s", s)
		}
	})

	t.Run("defaultModelGroup omitted when empty", func(t *testing.T) {
		cfg := SleepyRouterConfig{Port: 1, ModelGroups: ModelGroups{"g": {"m"}}, DefaultModelGroup: ""}
		data, _ := json.Marshal(cfg)
		if strings.Contains(string(data), "defaultModelGroup") {
			t.Errorf("defaultModelGroup should be omitted when empty: %s", string(data))
		}
	})

	// groups written in GroupOrder-then-alphabetical order
	t.Run("group order honored", func(t *testing.T) {
		cfg := SleepyRouterConfig{
			Port:        1,
			ModelGroups: ModelGroups{"a": {"x"}, "b": {"y"}, "c": {"z"}},
			GroupOrder:  []string{"c", "a"},
		}
		data, _ := json.Marshal(cfg)
		s := string(data)
		cIdx := strings.Index(s, `"c":`)
		aIdx := strings.Index(s, `"a":`)
		bIdx := strings.Index(s, `"b":`)
		if cIdx < 0 || aIdx <= cIdx || bIdx <= aIdx {
			t.Errorf("group order wrong (expected c<a<b): %s", s)
		}
	})
}
