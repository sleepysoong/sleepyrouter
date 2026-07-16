package sleepyrouter

import (
	"encoding/json"
	"testing"
)

func TestIsCachedFreeModel_NVIDIA(t *testing.T) {
	model := OmfmModel{ID: "nvidia/foo", Source: SourceNVIDIA}
	if !isCachedFreeModel(model) {
		t.Fatal("NVIDIA models should always be cached as free")
	}
}

func TestIsCachedFreeModel_Copilot(t *testing.T) {
	model := OmfmModel{ID: "copilot/gpt-4o", Source: SourceCopilot}
	if !isCachedFreeModel(model) {
		t.Fatal("Copilot models should always be cached as free")
	}
}

func TestIsCachedFreeModel_FreeSuffix(t *testing.T) {
	model := OmfmModel{ID: "meta-llama/llama-3:free"}
	if !isCachedFreeModel(model) {
		t.Fatal("Models with :free suffix should be cached as free")
	}
}

func TestIsCachedFreeModel_NonFreeRaw(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"id":      "paid/model",
		"pricing": map[string]any{"prompt": "1", "completion": "1"},
	})
	model := OmfmModel{ID: "paid/model", Source: SourceOpenRouter, Raw: raw}
	if isCachedFreeModel(model) {
		t.Fatal("Non-free OpenRouter model should not pass")
	}
}

func TestIsCachedFreeModel_FreeRaw(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"id":           "free/model:free",
		"architecture": map[string]any{"output_modalities": []any{"text"}},
	})
	model := OmfmModel{ID: "free/model:free", Source: SourceOpenRouter, Raw: raw}
	if !isCachedFreeModel(model) {
		t.Fatal("Free OpenRouter model with :free suffix should pass")
	}
}

func TestIsCachedFreeModel_FreeZeroPricingRaw(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"id":           "free/model",
		"architecture": map[string]any{"output_modalities": []any{"text"}},
		"pricing":      map[string]any{"prompt": "0", "completion": "0", "request": "0"},
	})
	model := OmfmModel{ID: "free/model", Source: SourceOpenRouter, Raw: raw}
	if !isCachedFreeModel(model) {
		t.Fatal("Free OpenRouter model with zero pricing should pass")
	}
}

func TestIsCachedFreeModel_NoRaw(t *testing.T) {
	model := OmfmModel{ID: "some/model", Source: SourceOpenRouter}
	if isCachedFreeModel(model) {
		t.Fatal("OpenRouter model without :free suffix and no raw should not pass")
	}
}

func TestIsFreeOpenRouterModelRaw_ImageOnly(t *testing.T) {
	raw := map[string]any{
		"id":           "image-model",
		"architecture": map[string]any{"output_modalities": []any{"image"}},
	}
	if isFreeOpenRouterModelRaw(raw) {
		t.Fatal("image-only model should not be free")
	}
}

func TestIsFreeOpenRouterModelRaw_EmptyOutputs(t *testing.T) {
	raw := map[string]any{
		"id": "text-model:free",
	}
	if !isFreeOpenRouterModelRaw(raw) {
		t.Fatal("model with :free suffix and no output_modalities should be free")
	}
}
