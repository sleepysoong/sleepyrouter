package cfg

import (
	"os"
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

func tempConfigStore(t *testing.T) (*ConfigStore, func()) {
	t.Helper()
	root, err := os.MkdirTemp("", "sleepyrouter-config-")
	if err != nil {
		t.Fatal(err)
	}
	store := NewConfigStore(root)
	return store, func() { os.RemoveAll(root) }
}

func TestConfigStore_ReadConfig_Defaults(t *testing.T) {
	store, cleanup := tempConfigStore(t)
	defer cleanup()
	config, err := store.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Port != DefaultPort {
		t.Fatalf("port: %d", config.Port)
	}
	if len(config.ModelGroups) != 0 {
		t.Fatalf("modelGroups not empty: %v", config.ModelGroups)
	}
}

func TestConfigStore_WriteAndReadConfig(t *testing.T) {
	store, cleanup := tempConfigStore(t)
	defer cleanup()
	config := types.SleepyRouterConfig{
		Port:         4567,
		ModelGroups:  types.ModelGroups{"fast": {"a:free", "b:free"}},
		DefaultModelGroup: "fast",
		GroupOrder:   []string{"fast"},
	}
	if err := store.WriteConfig(config); err != nil {
		t.Fatal(err)
	}
	read, err := store.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if read.Port != 4567 {
		t.Fatalf("port: %d", read.Port)
	}
	if len(read.ModelGroups["fast"]) != 2 || read.ModelGroups["fast"][0] != "a:free" {
		t.Fatalf("groups: %v", read.ModelGroups)
	}
	if read.DefaultModelGroup != "fast" {
		t.Fatalf("defaultModelGroup: %s", read.DefaultModelGroup)
	}
}

func TestConfigStore_UpdateModelGroup(t *testing.T) {
	store, cleanup := tempConfigStore(t)
	defer cleanup()
	config, err := store.UpdateModelGroup("coding", []string{"model-a", "model-b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(config.ModelGroups["coding"]) != 2 {
		t.Fatalf("coding: %v", config.ModelGroups["coding"])
	}
}

func TestConfigStore_UpdateModelGroupDeduplicates(t *testing.T) {
	store, cleanup := tempConfigStore(t)
	defer cleanup()
	config, err := store.UpdateModelGroup("coding", []string{"model-a", "model-b", "model-a"})
	if err != nil {
		t.Fatal(err)
	}
	ids := config.ModelGroups["coding"]
	if len(ids) != 2 || ids[0] != "model-a" || ids[1] != "model-b" {
		t.Fatalf("deduplicated: %v", ids)
	}
}

func TestConfigStore_UpdateModelGroupPreservesOrder(t *testing.T) {
	store, cleanup := tempConfigStore(t)
	defer cleanup()
	_, _ = store.UpdateModelGroup("coding", []string{"c", "b", "a"})
	config, err := store.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	ids := config.ModelGroups["coding"]
	if len(ids) != 3 || ids[0] != "c" || ids[1] != "b" || ids[2] != "a" {
		t.Fatalf("order: %v", ids)
	}
}

func TestConfigStore_UsageLogs(t *testing.T) {
	store, cleanup := tempConfigStore(t)
	defer cleanup()
	_ = store.AppendUsage(types.UsageLogEntry{TS: "2026-06-28T10:00:00Z", Model: "alpha", InputTokens: 1, OutputTokens: 2, Success: true})
	_ = store.AppendUsage(types.UsageLogEntry{TS: "2026-06-28T10:01:00Z", Model: "alpha", InputTokens: 0, OutputTokens: 0, Success: false})
	logs, err := store.ReadUsageLogs()
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 {
		t.Fatalf("logs: %d", len(logs))
	}
	if logs[0].Model != "alpha" || logs[0].InputTokens != 1 {
		t.Fatalf("log[0]: %v", logs[0])
	}
	if logs[1].Success {
		t.Fatal("expected false")
	}
}

func TestConfigStore_ModelCacheFresh(t *testing.T) {
	store, cleanup := tempConfigStore(t)
	defer cleanup()
	_ = store.WriteModelCache(types.ModelCache{
		Models:    []types.SleepyRouterModel{{ID: "m:free", Name: "M"}},
		FetchedAt: "2026-06-28T10:00:00Z",
	})
	cache, err := store.ReadModelCache()
	if err != nil {
		t.Fatal(err)
	}
	if cache == nil {
		t.Fatal("cache is nil")
	}
	if len(cache.Models) != 1 {
		t.Fatalf("models: %d", len(cache.Models))
	}
}

func TestParseDotEnv(t *testing.T) {
	result := utils.ParseDotEnv("KEY1=val1\n# comment\nKEY2=val2")
	if result["KEY1"] != "val1" {
		t.Fatalf("KEY1: %s", result["KEY1"])
	}
	if result["KEY2"] != "val2" {
		t.Fatalf("KEY2: %s", result["KEY2"])
	}
	if len(result) != 2 {
		t.Fatalf("count: %d", len(result))
	}
}

func TestParseDotEnv_QuotedValues(t *testing.T) {
	result := utils.ParseDotEnv("KEY1=\"val one\"\nKEY2='val two'")
	if result["KEY1"] != "val one" {
		t.Fatalf("KEY1: %s", result["KEY1"])
	}
	if result["KEY2"] != "val two" {
		t.Fatalf("KEY2: %s", result["KEY2"])
	}
}

func TestParseDotEnv_Empty(t *testing.T) {
	result := utils.ParseDotEnv("")
	if len(result) != 0 {
		t.Fatalf("expected empty, got %v", result)
	}
}

func TestParseDotEnv_CommentsAndEmptyLines(t *testing.T) {
	result := utils.ParseDotEnv("\n# comment\n\nKEY1=val1\n\n")
	if len(result) != 1 {
		t.Fatalf("count: %d", len(result))
	}
	if result["KEY1"] != "val1" {
		t.Fatalf("KEY1: %s", result["KEY1"])
	}
}




