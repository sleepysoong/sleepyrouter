package server

// routes registers all endpoints. No Chat Completions client-facing API.
func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /ready", s.handleReady)
	s.mux.HandleFunc("GET /version", s.handleVersion)
	s.mux.HandleFunc("GET /v1/models", s.handleModels)
	s.mux.HandleFunc("POST /v1/responses", s.handleResponses)
	// Hoard: /v1/responses plus a routing trace (tried models, failure reasons).
	s.mux.HandleFunc("POST /hoard/v1/responses", s.handleHoardResponses)
	s.mux.HandleFunc("POST /v1/messages", s.handleMessages)
	s.mux.HandleFunc("POST /v1/messages/count_tokens", s.handleCountTokens)
}
