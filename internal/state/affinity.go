package state

import "github.com/sleepysoong/sleepyrouter/internal/config"

// Affinity wraps memory (+ optional sqlite persistence wired by server).
type Affinity struct {
	mem *Memory
}

func New(_ *config.RuntimeSnapshot) *Affinity { return &Affinity{mem: NewMemory(0)} }

func (a *Affinity) Set(v ResponseAffinity)                 { a.mem.Set(v) }
func (a *Affinity) Get(id string) (ResponseAffinity, bool) { return a.mem.Get(id) }
