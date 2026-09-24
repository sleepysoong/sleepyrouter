package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type tomlCapabilities struct {
	Tools     *bool `toml:"tools"`
	Vision    *bool `toml:"vision"`
	Reasoning *bool `toml:"reasoning"`
}

type tomlProvider struct {
	BaseURL   string            `toml:"base_url"`
	APIKeyEnv string            `toml:"api_key_env"`
	WireAPI   string            `toml:"wire_api"`
	Headers   map[string]string `toml:"headers"`
}

type tomlModel struct {
	Provider              string            `toml:"provider"`
	UpstreamModel         string            `toml:"upstream_model"`
	ReasoningEffort       string            `toml:"reasoning_effort"`
	ThinkingBudget        *int              `toml:"thinking_budget"`
	Capabilities          *tomlCapabilities `toml:"capabilities"`
	Extra                 map[string]any    `toml:"extra"`
	InputPricePerMillion  float64           `toml:"input_price_per_million"`
	OutputPricePerMillion float64           `toml:"output_price_per_million"`
}

type tomlFile struct {
	Version *int `toml:"version"`
	Server  *struct {
		Host string `toml:"host"`
		Port *int   `toml:"port"`
	} `toml:"server"`
	Routing *struct {
		DefaultGroup string `toml:"default_group"`
	} `toml:"routing"`
	Timeouts *struct {
		Request    string `toml:"request"`
		FirstEvent string `toml:"first_event"`
		StreamIdle string `toml:"stream_idle"`
	} `toml:"timeouts"`
	Logging *struct {
		Level  string `toml:"level"`
		Format string `toml:"format"`
	} `toml:"logging"`
	Usage *struct {
		Enabled *bool `toml:"enabled"`
	} `toml:"usage"`
	Providers map[string]tomlProvider `toml:"providers"`
	Models    map[string]tomlModel    `toml:"models"`
	Groups    map[string][]string     `toml:"groups"`
}

// orderedGroupKeys preserves TOML document order for [groups] keys.
func orderedGroupKeys(data []byte, groups map[string][]string) []string {
	// go-toml v2 unmarshals into map (loses order), so re-scan with a
	// lightweight ordered parse: decode into toml.OrderedMap via raw parse.
	// Fallback: sorted keys are NOT allowed per spec, so we parse the
	// [groups] table textually in document order.
	order := []string{}
	seen := map[string]bool{}
	// Use toml decode into map[string]any preserving order is complex;
	// instead scan lines for keys under [groups] header.
	inGroups := false
	for _, line := range splitLines(string(data)) {
		t := trimSpace(line)
		if t == "" || t[0] == '#' {
			continue
		}
		if len(t) > 1 && t[0] == '[' {
			// Strip trailing comments so "[groups] # comment" still matches.
			header := trimSpace(cutComment(t))
			inGroups = (header == "[groups]")
			continue
		}
		if !inGroups {
			continue
		}
		eq := indexByte(t, '=')
		if eq < 0 {
			continue
		}
		k := trimSpace(trimQuotes(trimSpace(t[:eq])))
		if _, ok := groups[k]; ok && !seen[k] {
			seen[k] = true
			order = append(order, k)
		}
	}
	// Append any missing (e.g. quoted differently) in map order fallback.
	for k := range groups {
		if !seen[k] {
			order = append(order, k)
		}
	}
	return order
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func trimSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	j := len(s)
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

func trimQuotes(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// cutComment strips a trailing "#" comment. Only used for table headers,
// where "#" cannot appear inside a bare key.
func cutComment(s string) string {
	if i := indexByte(s, '#'); i >= 0 {
		return s[:i]
	}
	return s
}

// Parse parses TOML bytes into Config with defaults applied.
func Parse(data []byte) (Config, error) {
	cfg := Defaults()
	var tf tomlFile
	if err := toml.NewDecoder(strings.NewReader(string(data))).DisallowUnknownFields().Decode(&tf); err != nil {
		return Config{}, fmt.Errorf("toml parse: %w", err)
	}
	if tf.Version != nil {
		cfg.Version = *tf.Version
	}
	if tf.Server != nil {
		if tf.Server.Host != "" {
			cfg.Server.Host = tf.Server.Host
		}
		if tf.Server.Port != nil {
			cfg.Server.Port = *tf.Server.Port
		}
	}
	if tf.Routing != nil {
		cfg.Routing.DefaultGroup = tf.Routing.DefaultGroup
	}
	if tf.Timeouts != nil {
		if tf.Timeouts.Request != "" {
			d, err := time.ParseDuration(tf.Timeouts.Request)
			if err != nil {
				return Config{}, fmt.Errorf("timeouts.request: %w", err)
			}
			cfg.Timeouts.Request = d
		}
		if tf.Timeouts.FirstEvent != "" {
			d, err := time.ParseDuration(tf.Timeouts.FirstEvent)
			if err != nil {
				return Config{}, fmt.Errorf("timeouts.first_event: %w", err)
			}
			cfg.Timeouts.FirstEvent = d
		}
		if tf.Timeouts.StreamIdle != "" {
			d, err := time.ParseDuration(tf.Timeouts.StreamIdle)
			if err != nil {
				return Config{}, fmt.Errorf("timeouts.stream_idle: %w", err)
			}
			cfg.Timeouts.StreamIdle = d
		}
	}
	if tf.Logging != nil {
		if tf.Logging.Level != "" {
			cfg.Logging.Level = tf.Logging.Level
		}
		if tf.Logging.Format != "" {
			cfg.Logging.Format = tf.Logging.Format
		}
	}
	if tf.Usage != nil && tf.Usage.Enabled != nil {
		cfg.Usage.Enabled = *tf.Usage.Enabled
	}
	if tf.Providers != nil {
		for id, p := range tf.Providers {
			defBase, defEnv := builtinProviderDefault(id)
			pc := ProviderConfig{Headers: map[string]string{}}
			pc.BaseURL = p.BaseURL
			if pc.BaseURL == "" {
				pc.BaseURL = defBase
			}
			pc.WireAPI = p.WireAPI
			if pc.WireAPI == "" {
				pc.WireAPI = "responses"
			}
			pc.APIKeyEnv = p.APIKeyEnv
			if pc.APIKeyEnv == "" {
				pc.APIKeyEnv = defEnv
			}
			if p.Headers != nil {
				pc.Headers = p.Headers
			} else {
				pc.Headers = map[string]string{}
			}
			cfg.Providers[id] = pc
		}
	}
	if tf.Models != nil {
		for id, m := range tf.Models {
			mc := ModelConfig{Extra: map[string]any{}}
			mc.Provider = m.Provider
			mc.UpstreamModel = m.UpstreamModel
			mc.ReasoningEffort = m.ReasoningEffort
			mc.ThinkingBudget = m.ThinkingBudget
			if m.Capabilities != nil {
				mc.Capabilities = ModelCapabilities{
					Tools:     ParseCapability(m.Capabilities.Tools),
					Vision:    ParseCapability(m.Capabilities.Vision),
					Reasoning: ParseCapability(m.Capabilities.Reasoning),
				}
			}
			if m.Extra != nil {
				mc.Extra = m.Extra
			}
			mc.InputPricePerMillion = m.InputPricePerMillion
			mc.OutputPricePerMillion = m.OutputPricePerMillion
			cfg.Models[id] = mc
		}
	}
	if tf.Groups != nil {
		cfg.Groups = map[string][]string{}
		for k, v := range tf.Groups {
			cp := make([]string, len(v))
			copy(cp, v)
			cfg.Groups[k] = cp
		}
		cfg.GroupOrd = orderedGroupKeys(data, cfg.Groups)
	}
	return cfg, nil
}

// LoadFile reads and parses a config file.
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return Parse(data)
}
