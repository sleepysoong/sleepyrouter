package server_test

import (
	"sync"
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
)

func TestConcurrentRoutingDuringReload(t *testing.T) {
	cfg, _ := config.Parse([]byte(`
version = 1
[routing]
default_group = "coding"
[models."zen/a"]
provider = "zen"
upstream_model = "a"
[models."nvidia/b"]
provider = "nvidia"
upstream_model = "b"
[groups]
coding = ["zen/a", "nvidia/b"]
`))
	store, _ := config.NewStore(cfg, nil, 1)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				snap := store.Current()
				_, _, _ = routing.ResolveCandidates(snap, routing.RouteRequest{RequestedModel: "coding"})
			}
		}()
	}
	for i := uint64(2); i < 20; i++ {
		_ = store.TryReload(cfg, nil, i)
	}
	wg.Wait()
}
