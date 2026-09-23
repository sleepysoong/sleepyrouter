package openai

import (
	"encoding/json"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// RewriteResponseModel sets client-facing model to the requested virtual model.
func RewriteResponseModel(raw []byte, requestedModel string) []byte {
	if requestedModel == "" {
		return raw
	}
	out, err := sjson.SetBytes(raw, "model", requestedModel)
	if err != nil {
		return raw
	}
	return out
}

// StreamErrorEvent is the Responses "error" SSE payload, not a fabricated
// response.failed event (which requires a complete Response object).
func StreamErrorEvent(message string, sequenceNumber int64) []byte {
	b, _ := json.Marshal(struct {
		Type           string  `json:"type"`
		Code           string  `json:"code"`
		Message        string  `json:"message"`
		Param          *string `json:"param"`
		SequenceNumber int64   `json:"sequence_number"`
	}{Type: "error", Code: "upstream_error", Message: message, SequenceNumber: sequenceNumber})
	return b
}

// RewriteStreamEventModel rewrites only the nested response model on lifecycle events.
func RewriteStreamEventModel(payload, requestedModel string) string {
	if requestedModel == "" {
		return payload
	}
	if !gjson.Get(payload, "response.model").Exists() {
		return payload
	}
	out, err := sjson.Set(payload, "response.model", requestedModel)
	if err != nil {
		return payload
	}
	return out
}
