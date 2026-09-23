package config_test

import (
	"testing"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/config"
)

func TestDefaultStreamIdleExceedsClaudeCodeWatchdog(t *testing.T) {
	if got, want := config.Defaults().Timeouts.StreamIdle, 330*time.Second; got != want {
		t.Fatalf("default stream idle = %s, want %s", got, want)
	}
}

const exampleTOML = `
version = 1

[server]
host = "127.0.0.1"
port = 4567

[routing]
default_group = "coding"
unknown_model_policy = "default_group"

[providers.zen]
enabled = true
base_url = "https://example.invalid/v1"
api_key_env = "OPENCODE_API_KEY"

[providers.nvidia]
enabled = true
base_url = "https://example.invalid/v1"
api_key_env = "NVIDIA_API_KEY"

[models."zen/model-a"]
provider = "zen"
upstream_model = "model-a"

[models."nvidia/model-b"]
provider = "nvidia"
upstream_model = "vendor/model-b"

[models."gemini/model-c"]
provider = "gemini"
upstream_model = "model-c"

[groups]
coding = ["zen/model-a", "nvidia/model-b"]
fast = ["gemini/model-c", "nvidia/model-b"]

[aliases]
"claude-sleepy" = "coding"
`

func TestParsePreservesGroupOrder(t *testing.T) {
	cfg, err := config.Parse([]byte(exampleTOML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.GroupOrd) != 2 || cfg.GroupOrd[0] != "coding" || cfg.GroupOrd[1] != "fast" {
		t.Fatalf("group order = %v, want [coding fast]", cfg.GroupOrd)
	}
	if err := config.Validate(&cfg); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidationRejectsUnknownModel(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version = 1
[models."a/x"]
provider = "zen"
upstream_model = "x"
[groups]
coding = ["a/x", "missing/y"]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := config.Validate(&cfg); err == nil {
		t.Fatal("expected validation error for unknown model")
	}
}

func TestValidationRejectsDuplicate(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version = 1
[models."a/x"]
provider = "zen"
upstream_model = "x"
[groups]
coding = ["a/x", "a/x"]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := config.Validate(&cfg); err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestValidationRejectsCollision(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version = 1
[models."coding"]
provider = "zen"
upstream_model = "x"
[groups]
coding = ["coding"]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := config.Validate(&cfg); err == nil {
		t.Fatal("expected collision error")
	}
}

func TestValidationRejectsAnthropicThinkingBudgetForResponsesUpstream(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version = 1
[models."zen/model-a"]
provider = "zen"
upstream_model = "model-a"
thinking_budget = 1024
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := config.Validate(&cfg); err == nil {
		t.Fatal("expected incompatible thinking_budget validation error")
	}
}

func TestSnapshotResolvesKeys(t *testing.T) {
	t.Setenv("SLEEPYROUTER_TEST_KEY", "secret-from-env")
	cfg, _ := config.Parse([]byte(`
version = 1
[providers.custom]
enabled = true
base_url = "https://example.invalid/v1"
api_key_env = "SLEEPYROUTER_TEST_KEY"
[models."custom/m"]
provider = "custom"
upstream_model = "m"
[groups]
g = ["custom/m"]
`))
	dotenv := map[string]string{"SLEEPYROUTER_TEST_KEY": "from-dotenv"}
	snap := config.BuildSnapshot(cfg, dotenv, 1)
	if snap.Providers["custom"].APIKey != "secret-from-env" {
		t.Fatalf("process env should win, got %q", snap.Providers["custom"].APIKey)
	}
	snap2 := config.BuildSnapshot(cfg, dotenv, 2)
	// Without process env the dotenv fallback would apply; just check generation.
	if snap2.Generation != 2 {
		t.Fatalf("generation = %d", snap2.Generation)
	}
}

func TestInvalidReloadKeepsOld(t *testing.T) {
	cfg, _ := config.Parse([]byte(exampleTOML))
	store, err := config.NewStore(cfg, nil, 3)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	bad, _ := config.Parse([]byte("version = 1\n[groups]\ncoding = [\"nope/m\"]\n"))
	if err := store.TryReload(bad, nil, 4); err == nil {
		t.Fatal("expected reload failure")
	}
	if store.Generation() != 3 {
		t.Fatalf("generation = %d, want 3", store.Generation())
	}
}

func TestGroupOrderReversedAndCommented(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version = 1
[models."zen/a"]
provider = "zen"
upstream_model = "a"
[models."nvidia/b"]
provider = "nvidia"
upstream_model = "b"
[groups] # trailing comment must not break order detection
zebra = ["nvidia/b"]
alpha = ["zen/a"]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.GroupOrd) != 2 || cfg.GroupOrd[0] != "zebra" || cfg.GroupOrd[1] != "alpha" {
		t.Fatalf("group order = %v, want [zebra alpha] (document order, not alphabetical)", cfg.GroupOrd)
	}
}
