package providers

import (
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

func TestInferProvider(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"google/gemini-2.5-flash", "google"},
		{"meta-llama/llama-3", "meta-llama"},
		{"no-slash", "openrouter"},
		{"", "openrouter"},
		{"a/b/c", "a"},
	}
	for _, tc := range tests {
		if got := InferProvider(tc.input); got != tc.expected {
			t.Errorf("InferProvider(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestPriceIsZero_String(t *testing.T) {
	if !priceIsZero("0") {
		t.Fatal("expected true for '0'")
	}
	if priceIsZero("0.001") {
		t.Fatal("expected false for '0.001'")
	}
	if priceIsZero("") {
		t.Fatal("expected false for ''")
	}
	if priceIsZero(nil) {
		t.Fatal("expected false for nil")
	}
}

func TestPriceIsZero_Number(t *testing.T) {
	if !priceIsZero(float64(0)) {
		t.Fatal("expected true for 0")
	}
	if priceIsZero(float64(1)) {
		t.Fatal("expected false for 1")
	}
}

func TestIsFreeOpenRouterModel_FreeSuffix(t *testing.T) {
	model := OpenRouterModel{
		ID:           "meta-llama/llama-3:free",
		Architecture: OpenRouterArchitecture{OutputModalities: []string{"text"}},
	}
	if !IsFreeOpenRouterModel(model) {
		t.Fatal("expected free")
	}
}

func TestIsFreeOpenRouterModel_NotFree(t *testing.T) {
	model := OpenRouterModel{
		ID:           "meta-llama/llama-3",
		Architecture: OpenRouterArchitecture{OutputModalities: []string{"text"}},
		Pricing:      map[string]any{"prompt": "0.001", "completion": "0.001"},
	}
	if IsFreeOpenRouterModel(model) {
		t.Fatal("expected not free")
	}
}

func TestIsFreeOpenRouterModel_ZeroPricing(t *testing.T) {
	model := OpenRouterModel{
		ID:           "meta-llama/llama-3",
		Architecture: OpenRouterArchitecture{OutputModalities: []string{"text"}},
		Pricing:      map[string]any{"prompt": "0", "completion": "0", "request": "0"},
	}
	if !IsFreeOpenRouterModel(model) {
		t.Fatal("expected free")
	}
}

func TestIsFreeOpenRouterModel_NoTextOutput(t *testing.T) {
	model := OpenRouterModel{
		ID:           "image-model",
		Architecture: OpenRouterArchitecture{OutputModalities: []string{"image"}},
	}
	if IsFreeOpenRouterModel(model) {
		t.Fatal("expected not free for image-only")
	}
}

func TestOpenRouterUsageID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"meta-llama/llama-3:free", "openrouter/llama-3"},
		{"google/gemini-2.5-flash", "openrouter/gemini-2.5-flash"},
		{"simple-model", "openrouter/simple-model"},
		{"a/b/c:free", "openrouter/b/c"},
		{"x/y/z", "openrouter/y/z"},
	}
	for _, tc := range tests {
		if got := openRouterUsageID(tc.input); got != tc.expected {
			t.Errorf("openRouterUsageID(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestNormalizeOpenRouterModel(t *testing.T) {
	t.Run("with id", func(t *testing.T) {
		ctx := 8192
		model := OpenRouterModel{
			ID:                  "meta-llama/llama-3:free",
			Name:                "LLaMA 3",
			Architecture:        OpenRouterArchitecture{OutputModalities: []string{"text"}},
			ContextLength:       float64(ctx),
			SupportedParameters: []string{"temperature"},
		}
		rank := 5
		result := NormalizeOpenRouterModel(model, &rank, nil)
		if result.ID != "openrouter/meta-llama/llama-3:free" {
			t.Fatalf("id: %s", result.ID)
		}
		if result.UpstreamID != "meta-llama/llama-3:free" {
			t.Fatalf("upstreamId: %s", result.UpstreamID)
		}
		if result.Name != "LLaMA 3" {
			t.Fatalf("name: %s", result.Name)
		}
		if result.Source != types.SourceOpenRouter {
			t.Fatalf("source: %v", result.Source)
		}
		if result.UsageID != "openrouter/llama-3" {
			t.Fatalf("usageId: %s", result.UsageID)
		}
		if result.ContextLength == nil || *result.ContextLength != 8192 {
			t.Fatalf("contextLength: %v", result.ContextLength)
		}
		if result.PopularityRank == nil || *result.PopularityRank != 5 {
			t.Fatalf("popularityRank: %v", result.PopularityRank)
		}
	})

	t.Run("missing id uses canonical_slug", func(t *testing.T) {
		model := OpenRouterModel{
			CanonicalSlug: "fallback-slug",
			Name:          "Fallback",
		}
		result := NormalizeOpenRouterModel(model, nil, nil)
		if result.ID != "openrouter/fallback-slug" {
			t.Fatalf("id: %s", result.ID)
		}
		if result.UpstreamID != "fallback-slug" {
			t.Fatalf("upstreamId: %s", result.UpstreamID)
		}
	})

	t.Run("missing name uses id", func(t *testing.T) {
		model := OpenRouterModel{ID: "some/model"}
		result := NormalizeOpenRouterModel(model, nil, nil)
		if result.Name != "some/model" {
			t.Fatalf("name: %s", result.Name)
		}
	})
}
