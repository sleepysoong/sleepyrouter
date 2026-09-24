package config

import (
	"fmt"
	"net/url"
	"strings"
)

// Validate checks the full config before activation. Any error rejects reload.
func Validate(cfg *Config) error {
	if cfg.Version != 1 {
		return fmt.Errorf("unsupported version %d (want 1)", cfg.Version)
	}
	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		return fmt.Errorf("server.port %d out of range 1-65535", cfg.Server.Port)
	}
	if cfg.Timeouts.Request <= 0 || cfg.Timeouts.FirstEvent <= 0 || cfg.Timeouts.StreamIdle <= 0 {
		return fmt.Errorf("timeouts must be positive durations")
	}
	// Provider checks.
	for id, p := range cfg.Providers {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("provider id must not be empty")
		}
		if p.BaseURL == "" {
			return fmt.Errorf("providers.%s: base_url is empty", id)
		}
		if _, err := url.ParseRequestURI(p.BaseURL); err != nil {
			return fmt.Errorf("providers.%s: invalid base_url %q", id, p.BaseURL)
		}
		if p.WireAPI != "" && p.WireAPI != "responses" && p.WireAPI != "chat_completions" {
			return fmt.Errorf("providers.%s: wire_api must be responses|chat_completions", id)
		}
	}
	// Model checks.
	for id, m := range cfg.Models {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("model id must not be empty")
		}
		if strings.TrimSpace(m.Provider) == "" {
			return fmt.Errorf("models.%q: provider is empty", id)
		}
		if _, ok := cfg.Providers[m.Provider]; !ok {
			return fmt.Errorf("models.%q: unknown provider %q", id, m.Provider)
		}
		if strings.TrimSpace(m.UpstreamModel) == "" {
			return fmt.Errorf("models.%q: upstream_model is empty", id)
		}
		if m.ThinkingBudget != nil {
			return fmt.Errorf("models.%q: thinking_budget is not supported as a model config default", id)
		}
	}
	// Group and model names must be disjoint.
	for g := range cfg.Groups {
		if _, ok := cfg.Models[g]; ok {
			return fmt.Errorf("name collision: group %q is also a model id", g)
		}
	}
	// Group checks.
	seenInGroup := map[string]map[string]bool{}
	for _, g := range cfg.GroupOrd {
		members := cfg.Groups[g]
		if seenInGroup[g] == nil {
			seenInGroup[g] = map[string]bool{}
		}
		for i, mid := range members {
			if _, ok := cfg.Models[mid]; !ok {
				return fmt.Errorf("groups.%s[%d]: unknown model %q", g, i, mid)
			}
			if seenInGroup[g][mid] {
				return fmt.Errorf("groups.%s: duplicate model %q", g, mid)
			}
			seenInGroup[g][mid] = true
		}
	}
	// Also validate groups not in GroupOrd (defensive).
	for g, members := range cfg.Groups {
		found := false
		for _, o := range cfg.GroupOrd {
			if o == g {
				found = true
				break
			}
		}
		if !found {
			for i, mid := range members {
				if _, ok := cfg.Models[mid]; !ok {
					return fmt.Errorf("groups.%s[%d]: unknown model %q", g, i, mid)
				}
			}
		}
	}
	// Default group.
	if cfg.Routing.DefaultGroup == "" {
		return fmt.Errorf("routing.default_group is required")
	}
	if _, ok := cfg.Groups[cfg.Routing.DefaultGroup]; !ok {
		return fmt.Errorf("routing.default_group %q does not exist", cfg.Routing.DefaultGroup)
	}
	return nil
}
