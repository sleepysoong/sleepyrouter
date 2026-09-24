// Package config defines TOML schema and runtime snapshot types.
package config

import "time"

// CapabilityState is a 3-state capability declaration.
type CapabilityState int

const (
	CapabilityUnknown CapabilityState = iota
	CapabilitySupported
	CapabilityUnsupported
)

func (c CapabilityState) String() string {
	switch c {
	case CapabilitySupported:
		return "supported"
	case CapabilityUnsupported:
		return "unsupported"
	default:
		return "unknown"
	}
}

// ParseCapability parses optional bool-ish capability values.
// nil/absent -> unknown. true -> supported, false -> unsupported.
func ParseCapability(v *bool) CapabilityState {
	if v == nil {
		return CapabilityUnknown
	}
	if *v {
		return CapabilitySupported
	}
	return CapabilityUnsupported
}

type ServerConfig struct {
	Host string
	Port int
}

type RoutingConfig struct {
	DefaultGroup string
}

type TimeoutsConfig struct {
	Request    time.Duration
	FirstEvent time.Duration
	StreamIdle time.Duration
}

type LoggingConfig struct {
	Level  string
	Format string
}

type UsageConfig struct {
	Enabled bool
}

type ProviderConfig struct {
	BaseURL   string
	APIKeyEnv string
	WireAPI   string
	Headers   map[string]string
}

type ModelCapabilities struct {
	Tools     CapabilityState
	Vision    CapabilityState
	Reasoning CapabilityState
}

type ModelConfig struct {
	Provider       string
	UpstreamModel  string
	ThinkingBudget *int
	Capabilities   ModelCapabilities
	Extra          map[string]any

	// Optional pricing for usage estimates.
	InputPricePerMillion  float64
	OutputPricePerMillion float64
}

// Config is the parsed user configuration.
type Config struct {
	Version  int
	Server   ServerConfig
	Routing  RoutingConfig
	Timeouts TimeoutsConfig
	Logging  LoggingConfig
	Usage    UsageConfig

	Providers map[string]ProviderConfig
	Models    map[string]ModelConfig
	Groups    map[string][]string
	GroupOrd  []string // definition order, never sorted
}

// RuntimeProvider is the resolved, immutable provider used per request.
type RuntimeProvider struct {
	ID      string
	BaseURL string
	APIKey  string
	WireAPI string
	Headers map[string]string
}

// RuntimeModel is the resolved, immutable model used per request.
type RuntimeModel struct {
	LocalID               string
	ProviderID            string
	UpstreamModel         string
	ThinkingBudget        *int
	Capabilities          ModelCapabilities
	Extra                 map[string]any
	InputPricePerMillion  float64
	OutputPricePerMillion float64
}

// RuntimeSnapshot is an immutable per-generation view of config.
type RuntimeSnapshot struct {
	Generation uint64
	Server     ServerConfig
	Routing    RoutingConfig
	Timeouts   TimeoutsConfig
	Providers  map[string]*RuntimeProvider
	Models     map[string]RuntimeModel
	Groups     map[string][]string
	GroupOrder []string
}

func Defaults() Config {
	return Config{
		Version: 1,
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 4567,
		},
		Routing: RoutingConfig{
			DefaultGroup: "",
		},
		Timeouts: TimeoutsConfig{
			Request:    180 * time.Second,
			FirstEvent: 45 * time.Second,
			// Claude Code's default byte-level stream watchdog is 300s. Keep
			// the gateway idle ceiling slightly beyond it and send SSE pings
			// while waiting for a quiet Responses upstream.
			StreamIdle: 330 * time.Second,
		},
		Logging:   LoggingConfig{Level: "info", Format: "text"},
		Usage:     UsageConfig{Enabled: true},
		Providers: map[string]ProviderConfig{},
		Models:    map[string]ModelConfig{},
		Groups:    map[string][]string{},
		GroupOrd:  []string{},
	}
}
