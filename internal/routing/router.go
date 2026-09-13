// Package routing resolves ordered failover candidates.
// It must not import protocol packages (Invariant 2).
package routing

import "github.com/sleepysoong/sleepyrouter/internal/config"

// Requirements describes model capabilities a request needs.
type Requirements struct {
	Tools     bool
	Vision    bool
	Reasoning bool
}

// RouteRequest is the minimal routing input.
type RouteRequest struct {
	RequestedModel string
	Requirements   Requirements
}

// Candidate is a single ordered failover target.
type Candidate struct {
	LocalModelID  string
	ProviderID    string
	UpstreamModel string
	Model         config.RuntimeModel
	Provider      *config.RuntimeProvider
}

// RouteReason describes how resolution happened.
type RouteReason string

const (
	ReasonGroup   RouteReason = "model-group"
	ReasonDirect  RouteReason = "direct-model"
	ReasonAlias   RouteReason = "alias"
	ReasonDefault RouteReason = "fallback-default-group"
)

// ErrorClass classifies upstream failures for failover decisions.
type ErrorClass int

const (
	ErrorClient ErrorClass = iota
	ErrorAuth
	ErrorRateLimit
	ErrorTimeout
	ErrorNetwork
	ErrorModelUnavailable
	ErrorUnsupportedFeature
	ErrorUpstream
	ErrorUnknown
)

func (e ErrorClass) String() string {
	switch e {
	case ErrorClient:
		return "client"
	case ErrorAuth:
		return "auth"
	case ErrorRateLimit:
		return "rate_limit"
	case ErrorTimeout:
		return "timeout"
	case ErrorNetwork:
		return "network"
	case ErrorModelUnavailable:
		return "model_unavailable"
	case ErrorUnsupportedFeature:
		return "unsupported_feature"
	case ErrorUpstream:
		return "upstream"
	default:
		return "unknown"
	}
}

// Failoverable reports whether the next candidate should be tried.
func (e ErrorClass) Failoverable() bool {
	switch e {
	case ErrorRateLimit, ErrorTimeout, ErrorNetwork, ErrorModelUnavailable, ErrorUnsupportedFeature, ErrorUpstream, ErrorAuth, ErrorUnknown:
		return true
	case ErrorClient:
		return false
	default:
		return true
	}
}
