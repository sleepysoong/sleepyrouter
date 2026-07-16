package sleepyrouter

import (
	"encoding/json"
	"net/http"
)

type ModelSource string

const (
	SourceOpenRouter ModelSource = "openrouter"
	SourceNVIDIA     ModelSource = "nvidia"
	SourceCopilot    ModelSource = "copilot"
)

type ModelGroups map[string][]string

type SleepyRouterModel struct {
	ID                  string          `json:"id"`
	UpstreamID          string          `json:"upstreamId,omitempty"`
	Name                string          `json:"name"`
	Provider            string          `json:"provider"`
	Source              ModelSource     `json:"source,omitempty"`
	UsageID             string          `json:"usageId,omitempty"`
	ContextLength       *int            `json:"contextLength,omitempty"`
	PopularityRank      *int            `json:"popularityRank,omitempty"`
	SupportedParameters []string        `json:"supportedParameters,omitempty"`
	Raw                 json.RawMessage `json:"raw,omitempty"`
}

func SourceOf(model SleepyRouterModel) ModelSource {
	if model.Source == SourceNVIDIA {
		return SourceNVIDIA
	}
	if model.Source == SourceCopilot {
		return SourceCopilot
	}
	return SourceOpenRouter
}

type UsageLogEntry struct {
	TS           string `json:"ts"`
	Model        string `json:"model"`
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
	Success      bool   `json:"success"`
}

type SleepyRouterConfig struct {
	Port         int         `json:"port"`
	ModelGroups  ModelGroups `json:"modelGroups"`
	DefaultGroup string      `json:"defaultGroup,omitempty"`
	GroupOrder   []string    `json:"-"`
}

type ModelCache struct {
	Models    []SleepyRouterModel `json:"models"`
	FetchedAt string              `json:"fetchedAt"`
}

type ProviderAPIKeys struct {
	OpenRouter string
	NVIDIA     string
	Copilot    string
}

func (keys ProviderAPIKeys) For(source ModelSource) string {
	switch source {
	case SourceNVIDIA:
		return keys.NVIDIA
	case SourceCopilot:
		return keys.Copilot
	default:
		return keys.OpenRouter
	}
}

// HTTPDoer is deliberately the small http.Client surface needed by providers.
// Tests can inject a RoundTripper-backed client without opening the network.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
