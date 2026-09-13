package provider

import "github.com/sleepysoong/sleepyrouter/internal/config"

// Registry holds provider profiles; custom providers work without code.
type Registry struct {
	profiles map[string]Profile
}

func NewRegistry() *Registry { return &Registry{profiles: map[string]Profile{}} }

func DefaultRegistry() *Registry {
	r := NewRegistry()
	for _, id := range []string{"zen", "nvidia", "gemini", "openrouter"} {
		r.Register(ForID(id))
	}
	return r
}

// ForID returns the profile for a provider (generic fallback for custom).
func ForID(id string) Profile {
	switch id {
	case "zen":
		return ZenProfile()
	case "nvidia":
		return NvidiaProfile()
	case "gemini":
		return GeminiProfile()
	case "openrouter":
		return OpenRouterProfile()
	default:
		return defaultProfile(id)
	}
}

func (r *Registry) Register(p Profile) { r.profiles[p.ID] = p }

func (r *Registry) Get(id string) Profile {
	if p, ok := r.profiles[id]; ok {
		return p
	}
	return defaultProfile(id)
}

// ResolveEnv returns the api_key_env for a provider given config snapshot.
func ResolveEnv(providers map[string]*config.RuntimeProvider, id string) string {
	if p, ok := providers[id]; ok && p != nil {
		return p.APIKey
	}
	return ""
}
