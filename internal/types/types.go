package types

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
)

type ModelSource string

const (
	SourceOpenRouter ModelSource = "openrouter"
	SourceNVIDIA     ModelSource = "nvidia"
	SourceCopilot    ModelSource = "copilot"
	SourceZen        ModelSource = "zen"
)

// AllModelSources is the canonical registration order used by provider_registry
// and CLI validation. Order affects catalog fetch priority.
var AllModelSources = []ModelSource{SourceOpenRouter, SourceNVIDIA, SourceCopilot, SourceZen}

type ModelGroups map[string][]string

type SleepyRouterModel struct {
	ID         string      `json:"id"`
	UpstreamID string      `json:"upstreamId,omitempty"`
	Name       string      `json:"name"`
	Provider   string      `json:"provider"`
	Source     ModelSource `json:"source,omitempty"`
	UsageID    string      `json:"usageId,omitempty"`
}

func SourceOf(model SleepyRouterModel) ModelSource {
	switch model.Source {
	case SourceNVIDIA, SourceCopilot, SourceZen:
		return model.Source
	default:
		return SourceOpenRouter
	}
}

type UsageLogEntry struct {
	TS           string `json:"ts"`
	Model        string `json:"model"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
	Success      bool   `json:"success"`
}

// TokenSpec carries per-direction token limits and pricing ($/1M tokens).
type TokenSpec struct {
	MaxTokens int     `json:"maxTokens,omitempty"`
	Price     float64 `json:"price,omitempty"`
}

// ModelDefinition is a config-defined model alias. Provider selects which
// upstream API to call; Name is the model ID the upstream expects.
type ModelDefinition struct {
	Provider     string    `json:"provider"`
	Name         string    `json:"name"`
	InputTokens  TokenSpec `json:"inputTokens,omitempty"`
	OutputTokens TokenSpec `json:"outputTokens,omitempty"`
}

type SleepyRouterConfig struct {
	Port             int                        `json:"port"`
	ModelGroups      ModelGroups                `json:"modelGroups"`
	DefaultModelGroup string                    `json:"defaultModelGroup,omitempty"`
	GroupOrder       []string                   `json:"-"`
	Models           map[string]ModelDefinition `json:"models,omitempty"`
}

type ProviderAPIKeys struct {
	OpenRouter string
	NVIDIA     string
	Copilot    string
	Zen        string
}

func (keys ProviderAPIKeys) For(source ModelSource) string {
	switch source {
	case SourceNVIDIA:
		return keys.NVIDIA
	case SourceCopilot:
		return keys.Copilot
	case SourceZen:
		return keys.Zen
	default:
		return keys.OpenRouter
	}
}

// HTTPDoer is deliberately the small http.Client surface needed by providers.
// Tests can inject a RoundTripper-backed client without opening the network.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func CompleteGroupOrder(groups ModelGroups, preferred []string) []string {
	seen := make(map[string]bool)
	order := make([]string, 0, len(groups))
	for _, name := range preferred {
		if !seen[name] {
			if _, ok := groups[name]; ok {
				seen[name] = true
				order = append(order, name)
			}
		}
	}
	remaining := make([]string, 0, len(groups)-len(order))
	for name := range groups {
		if !seen[name] {
			remaining = append(remaining, name)
		}
	}
	sort.Strings(remaining)
	return append(order, remaining...)
}

func (config SleepyRouterConfig) MarshalJSON() ([]byte, error) {
	groups := config.ModelGroups
	if groups == nil {
		groups = ModelGroups{}
	}
	order := CompleteGroupOrder(groups, config.GroupOrder)
	var out bytes.Buffer
	out.WriteString(`{"port":`)
	out.WriteString(strconv.Itoa(config.Port))
	out.WriteString(`,"modelGroups":{`)
	for index, name := range order {
		if index > 0 {
			out.WriteByte(',')
		}
		key, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		ids, err := json.Marshal(groups[name])
		if err != nil {
			return nil, err
		}
		out.Write(key)
		out.WriteByte(':')
		out.Write(ids)
	}
	out.WriteByte('}')
	if config.DefaultModelGroup != "" {
		value, err := json.Marshal(config.DefaultModelGroup)
		if err != nil {
			return nil, err
		}
		out.WriteString(`,"defaultModelGroup":`)
		out.Write(value)
	}
	if len(config.Models) > 0 {
		modelsJSON, err := json.Marshal(config.Models)
		if err != nil {
			return nil, err
		}
		out.WriteString(`,"models":`)
		out.Write(modelsJSON)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}
