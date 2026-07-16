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
	if config.DefaultGroup != "" {
		value, err := json.Marshal(config.DefaultGroup)
		if err != nil {
			return nil, err
		}
		out.WriteString(`,"defaultGroup":`)
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}
