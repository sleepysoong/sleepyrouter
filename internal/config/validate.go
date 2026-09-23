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
	if cfg.Server.RequestBodyLimitMB <= 0 {
		cfg.Server.RequestBodyLimitMB = 32
	}
	if cfg.Timeouts.Request <= 0 || cfg.Timeouts.FirstEvent <= 0 || cfg.Timeouts.StreamIdle <= 0 {
		return fmt.Errorf("timeouts must be positive durations")
	}
	if cfg.Routing.UnknownModelPolicy != "" && cfg.Routing.UnknownModelPolicy != "default_group" && cfg.Routing.UnknownModelPolicy != "error" {
		return fmt.Errorf("routing.unknown_model_policy must be default_group|error")
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
			return fmt.Errorf("models.%q: thinking_budget is Anthropic-specific and cannot be represented by the Responses upstream; use reasoning_effort", id)
		}
	}
	// Collision checks: group/model/alias names must be disjoint.
	for g := range cfg.Groups {
		if _, ok := cfg.Models[g]; ok {
			return fmt.Errorf("name collision: group %q is also a model id", g)
		}
		if _, ok := cfg.Aliases[g]; ok {
			return fmt.Errorf("name collision: group %q is also an alias", g)
		}
	}
	for m := range cfg.Models {
		if _, ok := cfg.Aliases[m]; ok {
			return fmt.Errorf("name collision: model %q is also an alias", m)
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
	if cfg.Routing.DefaultGroup != "" {
		if _, ok := cfg.Groups[cfg.Routing.DefaultGroup]; !ok {
			return fmt.Errorf("routing.default_group %q does not exist", cfg.Routing.DefaultGroup)
		}
	}
	// Alias checks.
	for a, target := range cfg.Aliases {
		if strings.TrimSpace(a) == "" {
			return fmt.Errorf("alias name must not be empty")
		}
		if _, ok := cfg.Groups[target]; !ok {
			if _, ok2 := cfg.Models[target]; !ok2 {
				return fmt.Errorf("aliases.%q: unknown target %q", a, target)
			}
		}
	}
	return nil
}
