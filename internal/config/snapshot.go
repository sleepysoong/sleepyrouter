package config

import (
	"sync/atomic"
)

// Store holds the active immutable snapshot.
type Store struct {
	ptr atomic.Pointer[RuntimeSnapshot]
}

// NewStore builds the initial snapshot. dotenv may be nil.
func NewStore(cfg Config, dotenv map[string]string, generation uint64) (*Store, error) {
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	s := &Store{}
	s.Swap(BuildSnapshot(cfg, dotenv, generation))
	return s, nil
}

// Current returns the active snapshot (single fetch per request).
func (s *Store) Current() *RuntimeSnapshot { return s.ptr.Load() }

// Swap atomically replaces the snapshot.
func (s *Store) Swap(snap *RuntimeSnapshot) { s.ptr.Store(snap) }

// Generation returns current generation or 0.
func (s *Store) Generation() uint64 {
	if cur := s.ptr.Load(); cur != nil {
		return cur.Generation
	}
	return 0
}

// TryReload parses+validates and swaps on success; on failure keeps old.
func (s *Store) TryReload(cfg Config, dotenv map[string]string, generation uint64) error {
	if err := Validate(&cfg); err != nil {
		return err
	}
	s.Swap(BuildSnapshot(cfg, dotenv, generation))
	return nil
}

// BuildSnapshot resolves API keys (process env wins) into immutable runtime view.
func BuildSnapshot(cfg Config, dotenv map[string]string, generation uint64) *RuntimeSnapshot {
	if dotenv == nil {
		dotenv = map[string]string{}
	}
	snap := &RuntimeSnapshot{
		Generation: generation,
		Server:     cfg.Server,
		Routing:    cfg.Routing,
		Timeouts:   cfg.Timeouts,
		Providers:  map[string]*RuntimeProvider{},
		Models:     map[string]RuntimeModel{},
		Groups:     map[string][]string{},
	}
	for id, p := range cfg.Providers {
		envName := p.APIKeyEnv
		if envName == "" {
			envName = DefaultAPIKeyEnv(id)
		}
		key, _ := LookupEnv(envName, dotenv)
		hdr := map[string]string{}
		for k, v := range p.Headers {
			hdr[k] = v
		}
		snap.Providers[id] = &RuntimeProvider{
			ID: id, BaseURL: p.BaseURL, APIKey: key, WireAPI: p.WireAPI, Headers: hdr,
		}
	}
	for id, m := range cfg.Models {
		snap.Models[id] = RuntimeModel{
			LocalID: id, ProviderID: m.Provider, UpstreamModel: m.UpstreamModel,
			ThinkingBudget: m.ThinkingBudget, Capabilities: m.Capabilities,
			Extra:                m.Extra,
			InputPricePerMillion: m.InputPricePerMillion, OutputPricePerMillion: m.OutputPricePerMillion,
		}
	}
	for g, members := range cfg.Groups {
		cp := make([]string, len(members))
		copy(cp, members)
		snap.Groups[g] = cp
	}
	snap.GroupOrder = append([]string{}, cfg.GroupOrd...)
	return snap
}
