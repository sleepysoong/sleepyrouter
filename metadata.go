package sleepyrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const ModelMetadataRawURL = "https://raw.githubusercontent.com/hakilee/sleepyrouter/model-metadata/data/model-metadata.json"

const modelMetadataTimeout = 1200 * time.Millisecond

type ProviderModelMetadata struct {
	Source          ModelSource `json:"source"`
	ID              string      `json:"id"`
	Name            string      `json:"name,omitempty"`
	ContextLength   *int        `json:"contextLength,omitempty"`
	MetadataSources []string    `json:"metadataSources,omitempty"`
	UpdatedAt       string      `json:"updatedAt,omitempty"`
}

type ProviderMetadataCatalog map[string]ProviderModelMetadata

var (
	metadataCacheMu     sync.Mutex
	cachedLocalCatalog  ProviderMetadataCatalog
	cachedRemoteCatalog ProviderMetadataCatalog
	remoteCatalogLoaded bool
)

func metadataKey(source ModelSource, id string) string {
	return string(source) + ":" + strings.TrimSuffix(id, ":free")
}

func parseMetadataCatalog(data []byte) ProviderMetadataCatalog {
	var decoded struct {
		Models []struct {
			Source          ModelSource `json:"source"`
			ID              string      `json:"id"`
			Name            string      `json:"name"`
			ContextLength   any         `json:"contextLength"`
			MetadataSources []string    `json:"metadataSources"`
			UpdatedAt       string      `json:"updatedAt"`
		} `json:"models"`
	}
	if json.Unmarshal(data, &decoded) != nil {
		return ProviderMetadataCatalog{}
	}
	catalog := ProviderMetadataCatalog{}
	for _, model := range decoded.Models {
		if model.Source == "" || model.ID == "" {
			continue
		}
		catalog[metadataKey(model.Source, model.ID)] = ProviderModelMetadata{
			Source:          model.Source,
			ID:              model.ID,
			Name:            model.Name,
			ContextLength:   ParseTokenCount(model.ContextLength),
			MetadataSources: model.MetadataSources,
			UpdatedAt:       model.UpdatedAt,
		}
	}
	return catalog
}

func readLocalMetadataCatalog() ProviderMetadataCatalog {
	path := filepath.Join("data", "model-metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ProviderMetadataCatalog{}
	}
	return parseMetadataCatalog(data)
}

func localMetadataCatalog() ProviderMetadataCatalog {
	metadataCacheMu.Lock()
	defer metadataCacheMu.Unlock()
	if cachedLocalCatalog == nil {
		cachedLocalCatalog = readLocalMetadataCatalog()
	}
	return cachedLocalCatalog
}

func fetchRemoteMetadataCatalog(ctx context.Context, client HTTPDoer) ProviderMetadataCatalog {
	requestContext, cancel := context.WithTimeout(ctx, modelMetadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodGet, ModelMetadataRawURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "sleepyrouter/"+Version)
	response, err := client.Do(req)
	if err != nil || response == nil {
		return nil
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil
	}
	return parseMetadataCatalog(data)
}

func LoadModelMetadataCatalog(ctx context.Context, client HTTPDoer) ProviderMetadataCatalog {
	if client == nil {
		metadataCacheMu.Lock()
		if remoteCatalogLoaded {
			catalog := cachedRemoteCatalog
			metadataCacheMu.Unlock()
			return catalog
		}
		metadataCacheMu.Unlock()
		catalog := fetchRemoteMetadataCatalog(ctx, http.DefaultClient)
		if catalog == nil {
			catalog = localMetadataCatalog()
		}
		metadataCacheMu.Lock()
		if !remoteCatalogLoaded {
			cachedRemoteCatalog = catalog
			remoteCatalogLoaded = true
		}
		catalog = cachedRemoteCatalog
		metadataCacheMu.Unlock()
		return catalog
	}
	catalog := fetchRemoteMetadataCatalog(ctx, client)
	if catalog != nil {
		return catalog
	}
	return localMetadataCatalog()
}

func ModelMetadata(source ModelSource, id string, catalog ProviderMetadataCatalog) (ProviderModelMetadata, bool) {
	if catalog == nil {
		catalog = localMetadataCatalog()
	}
	model, ok := catalog[metadataKey(source, id)]
	return model, ok
}
