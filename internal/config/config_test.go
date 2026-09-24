package config_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/config"
)

func TestDefaultStreamIdleExceedsClaudeCodeWatchdog(t *testing.T) {
	if got, want := config.Defaults().Timeouts.StreamIdle, 330*time.Second; got != want {
		t.Fatalf("default stream idle = %s, want %s", got, want)
	}
}

func TestExampleConfigValid(t *testing.T) {
	data, err := os.ReadFile("../../config.example.toml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Validate(&cfg); err != nil {
		t.Fatal(err)
	}
}

func TestProviderWireAPIDefaultAndChatCompletions(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version = 1
[routing]
default_group = "coding"
[providers.nvidia-nim]
base_url = "https://example.invalid/v1"
api_key_env = "NVIDIA_NIM_API_KEY"
wire_api = "chat_completions"
[groups]
coding = []
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers["nvidia-nim"].WireAPI != "chat_completions" {
		t.Fatalf("wire_api = %q", cfg.Providers["nvidia-nim"].WireAPI)
	}
	if err := config.Validate(&cfg); err != nil {
		t.Fatal(err)
	}
	snap := config.BuildSnapshot(cfg, nil, 1)
	if got := snap.Providers["nvidia-nim"].WireAPI; got != "chat_completions" {
		t.Fatalf("runtime wire_api = %q", got)
	}

	defaultCfg, err := config.Parse([]byte("version = 1\n[providers.nvidia]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := defaultCfg.Providers["nvidia"].WireAPI; got != "responses" {
		t.Fatalf("declared provider default wire_api = %q, want responses", got)
	}
}

func TestValidationRejectsUnknownProviderWireAPI(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version = 1
[routing]
default_group = "coding"
[providers.custom]
base_url = "https://example.invalid/v1"
wire_api = "completions"
[groups]
coding = []
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Validate(&cfg); err == nil || !strings.Contains(err.Error(), "wire_api") {
		t.Fatalf("expected invalid wire_api error, got %v", err)
	}
}

func TestRemovedConfigKeysRejected(t *testing.T) {
	base := `version = 1
[routing]
default_group = "coding"
[providers.zen]
[models."zen/a"]
provider = "zen"
upstream_model = "a"
[groups]
coding = ["zen/a"]
`
	cases := map[string]string{
		"model_reasoning_effort": strings.Replace(base, "upstream_model = \"a\"", "upstream_model = \"a\"\nreasoning_effort = \"high\"", 1),
		"aliases":                base + "[aliases]\nold = \"coding\"\n",
		"unknown_model_policy":   strings.Replace(base, "default_group = \"coding\"", "default_group = \"coding\"\nunknown_model_policy = \"error\"", 1),
		"request_body_limit":     "version = 1\n[server]\nrequest_body_limit_mb = 32\n" + strings.TrimPrefix(base, "version = 1\n"),
		"shutdown_grace":         "version = 1\n[server]\nshutdown_grace = \"20s\"\n" + strings.TrimPrefix(base, "version = 1\n"),
		"provider_enabled":       strings.Replace(base, "[providers.zen]", "[providers.zen]\nenabled = false", 1),
		"model_enabled":          strings.Replace(base, "upstream_model = \"a\"", "upstream_model = \"a\"\nenabled = false", 1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := config.Parse([]byte(data)); err == nil {
				t.Fatal("removed config key was silently accepted")
			}
		})
	}
}

func TestDeclaredProvidersOnlyAndRequiredDefaultGroup(t *testing.T) {
	cfg, err := config.Parse([]byte(`version = 1
[routing]
default_group = "coding"
[providers.zen]
[models."zen/a"]
provider = "zen"
upstream_model = "a"
[groups]
coding = ["zen/a"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers) != 1 || cfg.Providers["zen"].BaseURL == "" {
		t.Fatalf("declared providers = %#v", cfg.Providers)
	}
	if err := config.Validate(&cfg); err != nil {
		t.Fatal(err)
	}
	delete(cfg.Providers, "zen")
	if err := config.Validate(&cfg); err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("expected dangling model provider error, got %v", err)
	}
	cfg.Providers["zen"] = config.ProviderConfig{BaseURL: "https://example.invalid/v1"}
	cfg.Routing.DefaultGroup = ""
	if err := config.Validate(&cfg); err == nil || !strings.Contains(err.Error(), "default_group") {
		t.Fatalf("expected missing default group error, got %v", err)
	}
}

const exampleTOML = `
version = 1

[server]
host = "127.0.0.1"
port = 4567

[routing]
default_group = "coding"

[providers.zen]
base_url = "https://example.invalid/v1"
api_key_env = "OPENCODE_API_KEY"

[providers.nvidia]
base_url = "https://example.invalid/v1"
api_key_env = "NVIDIA_API_KEY"

[providers.gemini]

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
[routing]
default_group = "coding"
[providers.zen]
[models."a/x"]
provider = "zen"
upstream_model = "x"
[groups]
coding = ["a/x", "missing/y"]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := config.Validate(&cfg); err == nil || !strings.Contains(err.Error(), "unknown model") {
		t.Fatalf("expected unknown model error, got %v", err)
	}
}

func TestValidationRejectsDuplicate(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version = 1
[routing]
default_group = "coding"
[providers.zen]
[models."a/x"]
provider = "zen"
upstream_model = "x"
[groups]
coding = ["a/x", "a/x"]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := config.Validate(&cfg); err == nil || !strings.Contains(err.Error(), "duplicate model") {
		t.Fatalf("expected duplicate model error, got %v", err)
	}
}

func TestValidationRejectsCollision(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version = 1
[routing]
default_group = "coding"
[providers.zen]
[models."coding"]
provider = "zen"
upstream_model = "x"
[groups]
coding = ["coding"]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := config.Validate(&cfg); err == nil || !strings.Contains(err.Error(), "name collision") {
		t.Fatalf("expected collision error, got %v", err)
	}
}

func TestValidationRejectsAnthropicThinkingBudgetForResponsesUpstream(t *testing.T) {
	cfg, err := config.Parse([]byte(`
version = 1
[routing]
default_group = "coding"
[providers.zen]
[models."zen/model-a"]
provider = "zen"
upstream_model = "model-a"
thinking_budget = 1024
[groups]
coding = ["zen/model-a"]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := config.Validate(&cfg); err == nil || !strings.Contains(err.Error(), "thinking_budget") {
		t.Fatalf("expected thinking_budget validation error, got %v", err)
	}
}

func TestSnapshotResolvesKeys(t *testing.T) {
	t.Setenv("SLEEPYROUTER_TEST_KEY", "secret-from-env")
	cfg, _ := config.Parse([]byte(`
version = 1
[routing]
default_group = "g"
[providers.custom]
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
[routing]
default_group = "zebra"
[providers.zen]
[providers.nvidia]
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
