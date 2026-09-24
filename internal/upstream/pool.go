package upstream

import (
	"github.com/openai/openai-go/v3"
	"github.com/sleepysoong/sleepyrouter/internal/config"
)

// Pool caches SDK clients per provider within a snapshot generation.
// Clients are goroutine-safe; never mutate base URL per request.
type Pool struct {
	clients map[string]openai.Client
}

func NewPool(snap *config.RuntimeSnapshot) *Pool {
	p := &Pool{clients: map[string]openai.Client{}}
	for id, rp := range snap.Providers {
		if rp == nil || rp.APIKey == "" || rp.BaseURL == "" {
			continue
		}
		p.clients[id] = NewClient(rp)
	}
	return p
}

func (p *Pool) Get(providerID string, rp *config.RuntimeProvider) (openai.Client, bool) {
	if c, ok := p.clients[providerID]; ok {
		return c, true
	}
	if rp == nil || rp.APIKey == "" {
		var z openai.Client
		return z, false
	}
	c := NewClient(rp)
	if p.clients == nil {
		p.clients = map[string]openai.Client{}
	}
	p.clients[providerID] = c
	return c, true
}
