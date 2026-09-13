package routing

import (
	"strings"

	"github.com/sleepysoong/sleepyrouter/internal/config"
)

// Resolve maps a requested model to an ordered local-model ID list.
// Order: exact group -> exact model -> alias -> unknown policy.
// Group array order is authoritative and never re-sorted.
func Resolve(snap *config.RuntimeSnapshot, requested string) ([]string, RouteReason, error) {
	req := strings.TrimSpace(requested)
	if req == "" {
		return nil, "", errMissingModel()
	}
	// 1. exact group match (case-sensitive per TOML keys; be strict)
	if members, ok := snap.Groups[req]; ok {
		out := append([]string{}, members...)
		return out, ReasonGroup, nil
	}
	// 2. exact local model match
	if _, ok := snap.Models[req]; ok {
		return []string{req}, ReasonDirect, nil
	}
	// 3. alias match
	if target, ok := snap.Aliases[req]; ok {
		if members, ok := snap.Groups[target]; ok {
			return append([]string{}, members...), ReasonAlias, nil
		}
		if _, ok := snap.Models[target]; ok {
			return []string{target}, ReasonAlias, nil
		}
		return nil, "", errUnknownAliasTarget(req, target)
	}
	// 4. unknown policy
	policy := snap.Routing.UnknownModelPolicy
	if policy == "" {
		policy = "default_group"
	}
	if policy == "error" {
		return nil, "", errUnknownModel(req)
	}
	def := snap.Routing.DefaultGroup
	if def == "" {
		return nil, "", errUnknownModel(req)
	}
	members, ok := snap.Groups[def]
	if !ok {
		return nil, "", errUnknownModel(req)
	}
	return append([]string{}, members...), ReasonDefault, nil
}

// FilterCandidates applies deterministic exclusion without reordering:
// disabled model, disabled provider, explicit capability unsupported.
func FilterCandidates(snap *config.RuntimeSnapshot, ids []string, req Requirements) []Candidate {
	out := make([]Candidate, 0, len(ids))
	for _, id := range ids {
		m, ok := snap.Models[id]
		if !ok || !m.Enabled {
			continue
		}
		p, ok := snap.Providers[m.ProviderID]
		if !ok || !p.Enabled {
			continue
		}
		if req.Tools && m.Capabilities.Tools == config.CapabilityUnsupported {
			continue
		}
		if req.Vision && m.Capabilities.Vision == config.CapabilityUnsupported {
			continue
		}
		if req.Reasoning && m.Capabilities.Reasoning == config.CapabilityUnsupported {
			continue
		}
		out = append(out, Candidate{
			LocalModelID: id, ProviderID: m.ProviderID,
			UpstreamModel: m.UpstreamModel, Model: m, Provider: p,
		})
	}
	return out
}

// ResolveCandidates is Resolve + Filter in one call.
func ResolveCandidates(snap *config.RuntimeSnapshot, r RouteRequest) ([]Candidate, RouteReason, error) {
	ids, reason, err := Resolve(snap, r.RequestedModel)
	if err != nil {
		return nil, "", err
	}
	return FilterCandidates(snap, ids, r.Requirements), reason, nil
}
