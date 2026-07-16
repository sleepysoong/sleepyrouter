package sleepyrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func tempCatalogStore(t *testing.T) (*ConfigStore, func()) {
	t.Helper()
	root, err := os.MkdirTemp("", "slr-catalog-test-")
	if err != nil {
		t.Fatal(err)
	}
	return NewConfigStore(root), func() { os.RemoveAll(root) }
}

func catalogMockClient(fn func(req *http.Request) (*http.Response, error)) HTTPDoer {
	return httpClientFunc(fn)
}

func catalogJSONResponse(status int, body any) *http.Response {
	data, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(data)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func TestCatalog_DeduplicatesByLocalModelID(t *testing.T) {
	// Simulates NVIDIA models API returning duplicates
	mock := catalogMockClient(func(req *http.Request) (*http.Response, error) {
		url := req.URL.String()
		if url == ModelMetadataRawURL {
			return catalogJSONResponse(200, map[string]any{"models": []any{}}), nil
		}
		// NVIDIA models endpoint
		return catalogJSONResponse(200, map[string]any{
			"data": []any{
				map[string]any{"id": "deepseek-ai/deepseek-v4-pro", "name": "deepseek-v4-pro", "context_length": float64(1000000)},
				map[string]any{"id": "deepseek-ai/deepseek-v4-pro", "name": "deepseek-v4-pro", "context_length": float64(1000000)},
			},
		}), nil
	})

	result := ListAvailableFreeModels(context.Background(), ProviderAPIKeys{NVIDIA: "nvapi-key"}, mock)
	if len(result.Models) != 1 {
		t.Fatalf("expected 1 model, got %d: %v", len(result.Models), result.Models)
	}
	if result.Models[0].ID != "nvidia/deepseek-ai/deepseek-v4-pro" {
		t.Fatalf("id: %s", result.Models[0].ID)
	}
}

func TestCatalog_DeduplicatesFreshCachedModels(t *testing.T) {
	store, cleanup := tempCatalogStore(t)
	defer cleanup()

	duplicate := OmfmModel{
		ID:         "nvidia/deepseek-ai/deepseek-v4-pro",
		UpstreamID: "deepseek-ai/deepseek-v4-pro",
		Name:       "deepseek-v4-pro",
		Provider:   "nvidia",
		Source:     SourceNVIDIA,
	}
	_ = store.WriteModelCache(ModelCache{
		Models:    []OmfmModel{duplicate, duplicate},
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	})

	// Fresh cache should be used, no fetch needed
	catalog, err := LoadModelCatalog(context.Background(), ProviderAPIKeys{NVIDIA: "nvapi-key"}, nil, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(catalog.Models))
	}
	if catalog.Models[0].ID != "nvidia/deepseek-ai/deepseek-v4-pro" {
		t.Fatalf("id: %s", catalog.Models[0].ID)
	}
	if catalog.Source != "fresh" {
		t.Fatalf("source: %s", catalog.Source)
	}
}

func TestCatalog_FreshCacheFiltersByConfiguredProviders(t *testing.T) {
	store, cleanup := tempCatalogStore(t)
	defer cleanup()

	models := []OmfmModel{
		{ID: "nvidia/foo", Provider: "nvidia", Source: SourceNVIDIA},
		{ID: "openrouter/bar", Provider: "test", Source: SourceOpenRouter},
	}
	_ = store.WriteModelCache(ModelCache{
		Models:    models,
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	})

	// Only NVIDIA key configured, so openrouter model should be filtered out
	catalog, err := LoadModelCatalog(context.Background(), ProviderAPIKeys{NVIDIA: "nvapi-key"}, nil, store)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(catalog.Models))
	for i, m := range catalog.Models {
		ids[i] = m.ID
	}
	if len(ids) != 1 || ids[0] != "nvidia/foo" {
		t.Fatalf("expected [nvidia/foo], got %v", ids)
	}
}
