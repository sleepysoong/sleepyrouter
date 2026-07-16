package sleepyrouter

import (
	"testing"
)

func TestTitleFromID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"deepseek-ai/deepseek-v3.2", "Deepseek V3.2"},
		{"meta/llama-3_8b", "Llama 3 8b"},
		{"simple", "Simple"},
		{"a/b/c", "C"},
	}
	for _, tc := range tests {
		if got := titleFromID(tc.input); got != tc.expected {
			t.Errorf("titleFromID(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestIsChatLikeNVIDIAModel_Valid(t *testing.T) {
	model := map[string]any{"id": "deepseek-ai/deepseek-v3.2", "name": "DeepSeek"}
	if !IsChatLikeNVIDIAModel(model) {
		t.Fatal("expected true")
	}
}

func TestIsChatLikeNVIDIAModel_Embedding(t *testing.T) {
	model := map[string]any{"id": "nvidia/nv-embed-v1"}
	if IsChatLikeNVIDIAModel(model) {
		t.Fatal("expected false for embedding")
	}
}

func TestIsChatLikeNVIDIAModel_Rerank(t *testing.T) {
	model := map[string]any{"id": "nvidia/rerank-model"}
	if IsChatLikeNVIDIAModel(model) {
		t.Fatal("expected false for rerank")
	}
}

func TestIsChatLikeNVIDIAModel_EmptyID(t *testing.T) {
	model := map[string]any{"name": "Test"}
	if IsChatLikeNVIDIAModel(model) {
		t.Fatal("expected false for empty id")
	}
}

func TestNvidiaUsageID_StripsProviderPrefix(t *testing.T) {
	// strips first path element from upstreamId, no size pattern match for "v3.2"
	id := nvidiaUsageID("deepseek-ai/deepseek-v3.2")
	if id != "nvidia/deepseek-v3.2" {
		t.Fatalf("got %q, want nvidia/deepseek-v3.2", id)
	}
}

func TestNvidiaUsageID_NoSizeTag(t *testing.T) {
	// 51b is only 2 digits so the 3+-digit pattern doesn't match
	// Input is the upstream ID (after stripping nvidia/ prefix)
	id := nvidiaUsageID("llama-3.1-nemotron-51b-instruct")
	expected := "nvidia/llama-3.1-nemotron-51b-instruct"
	if id != expected {
		t.Fatalf("got %q, want %q", id, expected)
	}
}

func TestNvidiaUsageID_WithSizeTag(t *testing.T) {
	// Strips "270b" size tag
	id := nvidiaUsageID("google/gemma-2-270b-it")
	if id != "nvidia/gemma-2" {
		t.Fatalf("got %q, want nvidia/gemma-2", id)
	}
}

func TestNormalizeNVIDIAModel(t *testing.T) {
	model := map[string]any{
		"id":             "deepseek-ai/deepseek-v3.2",
		"name":           "DeepSeek V3.2",
		"context_length": float64(1000000),
	}
	result := NormalizeNVIDIAModel(model, nil)
	if result.ID != "nvidia/deepseek-ai/deepseek-v3.2" {
		t.Fatalf("id: %s", result.ID)
	}
	if result.UpstreamID != "deepseek-ai/deepseek-v3.2" {
		t.Fatalf("upstreamId: %s", result.UpstreamID)
	}
	if result.Provider != "nvidia" {
		t.Fatalf("provider: %s", result.Provider)
	}
	if result.Source != SourceNVIDIA {
		t.Fatalf("source: %v", result.Source)
	}
	if result.ContextLength == nil || *result.ContextLength != 1000000 {
		t.Fatalf("contextLength: %v", result.ContextLength)
	}
}

func TestNormalizeNVIDIAModel_MissingName(t *testing.T) {
	model := map[string]any{"id": "deepseek-ai/deepseek-v3.2"}
	result := NormalizeNVIDIAModel(model, nil)
	if result.Name == "" {
		t.Fatal("name should fallback to title from id")
	}
}

func TestNvidiaUsageID_MultiSlash(t *testing.T) {
	// "a/b/c" → provider "a" stripped, model "b/c" preserved
	id := nvidiaUsageID("a/b/c")
	if id != "nvidia/b/c" {
		t.Fatalf("got %q, want nvidia/b/c", id)
	}
}
