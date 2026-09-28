package server

import (
	"context"
	"encoding/json"

	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/protocol/openai"
	"github.com/sleepysoong/sleepyrouter/internal/routing"
)

func (s *Server) withReasoningObserver(ctx context.Context, requestID, model string, secrets []string) context.Context {
	if s.console == nil {
		return ctx
	}
	return openai.WithReasoningObserver(ctx, func(delta string) {
		s.console.ReasoningDelta(requestID, model, delta, secrets)
	})
}

func (s *Server) observeReasoningEvent(requestID string, candidate routing.Candidate, eventType string, payload []byte, secrets []string) {
	if s.console == nil || candidate.Provider != nil && candidate.Provider.WireAPI == "chat_completions" {
		return
	}
	switch eventType {
	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		var event struct {
			Delta string `json:"delta"`
		}
		if json.Unmarshal(payload, &event) == nil && event.Delta != "" {
			s.console.ReasoningDelta(requestID, candidate.LocalModelID, event.Delta, secrets)
		}
	}
}

func (s *Server) finishReasoningView(requestID, model string) {
	if s.console != nil {
		s.console.EndReasoning(requestID, model)
	}
}

func consoleSecrets(snap *config.RuntimeSnapshot) []string {
	if snap == nil {
		return nil
	}
	secrets := make([]string, 0, len(snap.Providers)+1)
	if snap.AuthToken != "" {
		secrets = append(secrets, snap.AuthToken)
	}
	for _, provider := range snap.Providers {
		if provider != nil && provider.APIKey != "" {
			secrets = append(secrets, provider.APIKey)
		}
	}
	return secrets
}
