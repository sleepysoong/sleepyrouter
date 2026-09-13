// Package provider defines provider profiles and compatibility hooks.
// It must not import protocol packages.
package provider

import (
	"context"

	"github.com/sleepysoong/sleepyrouter/internal/routing"
)

// UpstreamRequest is a hook-mutable request view (raw JSON preserved).
type UpstreamRequest struct {
	// RawBody is the full Responses API body with model already rewritten.
	RawBody []byte
	// Headers explicitly allowed to forward (never Authorization).
	Headers map[string]string
}

// CompatibilityHook customizes per-provider quirks.
type CompatibilityHook interface {
	PrepareRequest(ctx context.Context, c routing.Candidate, req *UpstreamRequest) error
	ClassifyError(status int, errType, code, message string) routing.ErrorClass
}

type noopHook struct{}

func (noopHook) PrepareRequest(context.Context, routing.Candidate, *UpstreamRequest) error {
	return nil
}
func (noopHook) ClassifyError(status int, errType, code, message string) routing.ErrorClass {
	return routing.ClassifyBody(status, errType, code, message)
}

// Profile is the static provider definition.
type Profile struct {
	ID   string
	Hook CompatibilityHook
}

func defaultProfile(id string) Profile { return Profile{ID: id, Hook: noopHook{}} }
