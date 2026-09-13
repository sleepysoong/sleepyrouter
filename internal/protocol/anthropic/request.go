package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sleepysoong/sleepyrouter/internal/routing"
)

// MessageRequest is the typed Anthropic Messages request.
type MessageRequest struct {
	Model         string                     `json:"model"`
	MaxTokens     int                        `json:"max_tokens"`
	Messages      []Message                  `json:"messages"`
	System        json.RawMessage            `json:"system"`
	Tools         []Tool                     `json:"tools"`
	ToolChoice    json.RawMessage            `json:"tool_choice"`
	Stream        bool                       `json:"stream"`
	Thinking      json.RawMessage            `json:"thinking"`
	Temperature   *float64                   `json:"temperature"`
	TopP          *float64                   `json:"top_p"`
	StopSequences []string                   `json:"stop_sequences"`
	Metadata      json.RawMessage            `json:"metadata"`
	Extra         map[string]json.RawMessage `json:"-"`
}

// Message is a single conversation turn.
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ContentBlock is a generic block.
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	Source    json.RawMessage `json:"source"`
}

// Tool is an Anthropic tool definition.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Parsed wraps raw + routing metadata + typed request.
type Parsed struct {
	Raw            []byte
	Typed          MessageRequest
	RequestedModel string
	Stream         bool
	Requirements   routing.Requirements
	SessionID      string
	AnthropicVer   string
	AnthropicBeta  string
}

// Parse validates and extracts routing metadata. Unknown content types are
// rejected explicitly (never silently dropped).
func Parse(raw []byte) (Parsed, error) {
	var v struct {
		Model       *string         `json:"model"`
		MaxTokens   *int            `json:"max_tokens"`
		Messages    json.RawMessage `json:"messages"`
		Stream      *bool           `json:"stream"`
		Tools       json.RawMessage `json:"tools"`
		Thinking    json.RawMessage `json:"thinking"`
		System      json.RawMessage `json:"system"`
		ToolChoice  json.RawMessage `json:"tool_choice"`
		Temperature *float64        `json:"temperature"`
		TopP        *float64        `json:"top_p"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return Parsed{}, fmt.Errorf("malformed JSON: %w", err)
	}
	if v.Model == nil || strings.TrimSpace(*v.Model) == "" {
		return Parsed{}, fmt.Errorf("model is required")
	}
	if v.MaxTokens == nil {
		return Parsed{}, fmt.Errorf("max_tokens is required")
	}
	var msgs []Message
	if err := json.Unmarshal(v.Messages, &msgs); err != nil || len(msgs) == 0 {
		// Try null/missing handling: must be non-empty list.
		return Parsed{}, fmt.Errorf("messages must be a non-empty list")
	}
	var typed MessageRequest
	if err := json.Unmarshal(raw, &typed); err != nil {
		return Parsed{}, fmt.Errorf("invalid request: %w", err)
	}
	p := Parsed{Raw: append([]byte{}, raw...), Typed: typed, RequestedModel: *v.Model}
	if v.Stream != nil {
		p.Stream = *v.Stream
	} else {
		p.Stream = typed.Stream
	}
	p.Requirements = deriveRequirements(raw, typed)
	if err := validateBlocks(typed); err != nil {
		return Parsed{}, err
	}
	return p, nil
}

func deriveRequirements(raw []byte, t MessageRequest) routing.Requirements {
	var r routing.Requirements
	if len(t.Tools) > 0 {
		r.Tools = true
	}
	if len(t.Thinking) > 0 && string(t.Thinking) != "null" {
		var th struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(t.Thinking, &th); err == nil && th.Type == "enabled" {
			r.Reasoning = true
		} else if err != nil {
			// Unknown thinking shape with content -> treat as reasoning need.
			r.Reasoning = true
		}
	}
	s := strings.ToLower(string(raw))
	if strings.Contains(s, "\"type\":\"image\"") || strings.Contains(s, "\"type\": \"image\"") {
		r.Vision = true
	}
	return r
}

func validateBlocks(t MessageRequest) error {
	for i, m := range t.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			return fmt.Errorf("messages[%d]: invalid role %q", i, m.Role)
		}
		trimmed := strings.TrimSpace(string(m.Content))
		if trimmed == "" || trimmed == "null" {
			return fmt.Errorf("messages[%d]: content is required", i)
		}
		if strings.HasPrefix(trimmed, "\"") {
			continue // string shorthand
		}
		var blocks []ContentBlock
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			return fmt.Errorf("messages[%d]: content must be string or block list", i)
		}
		for j, b := range blocks {
			switch b.Type {
			case "text", "image", "tool_use", "tool_result", "thinking", "redacted_thinking":
			case "":
				return fmt.Errorf("messages[%d].content[%d]: missing type", i, j)
			default:
				return fmt.Errorf("messages[%d].content[%d]: unknown content type %q", i, j, b.Type)
			}
		}
	}
	// Validate system: string or block list.
	if len(t.System) > 0 && string(t.System) != "null" {
		s := strings.TrimSpace(string(t.System))
		if !strings.HasPrefix(s, "\"") && !strings.HasPrefix(s, "[") {
			return fmt.Errorf("system must be string or block list")
		}
	}
	return nil
}
