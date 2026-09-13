package openai

import (
	"encoding/json"
	"net/http"
)

// ErrorEnvelope is the OpenAI-style error body.
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody mirrors OpenAI error shape.
type ErrorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   any    `json:"param"`
	Code    string `json:"code"`
}

// WriteError writes a protocol-compatible error.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorEnvelope{Error: ErrorBody{
		Message: message, Type: "upstream_error", Param: nil, Code: code,
	}})
}

// WriteBadRequest writes client errors.
func WriteBadRequest(w http.ResponseWriter, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(ErrorEnvelope{Error: ErrorBody{
		Message: message, Type: "invalid_request_error", Param: nil, Code: code,
	}})
}
