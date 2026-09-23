package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/sleepysoong/sleepyrouter/internal/routing"
)

// MessageRequest is the typed Anthropic Messages request.
type MessageRequest struct {
	Model         string          `json:"model"`
	MaxTokens     int             `json:"max_tokens"`
	Messages      []Message       `json:"messages"`
	System        json.RawMessage `json:"system"`
	Tools         []Tool          `json:"tools"`
	ToolChoice    json.RawMessage `json:"tool_choice"`
	Stream        bool            `json:"stream"`
	Thinking      json.RawMessage `json:"thinking"`
	OutputConfig  json.RawMessage `json:"output_config"`
	Temperature   *float64        `json:"temperature"`
	TopP          *float64        `json:"top_p"`
	StopSequences []string        `json:"stop_sequences"`
	Metadata      json.RawMessage `json:"metadata"`
}

// Message is a single conversation turn.
type Message struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	OutputConfig json.RawMessage `json:"output_config"`
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
	IsError   bool            `json:"is_error"`
	Title     string          `json:"title"`
}

// Tool is an Anthropic tool definition.
type Tool struct {
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	Strict       *bool           `json:"strict"`
	DeferLoading *bool           `json:"defer_loading"`
}

// Parsed wraps raw + routing metadata + typed request.
type Parsed struct {
	Raw            []byte
	Typed          MessageRequest
	RequestedModel string
	Stream         bool
	Requirements   routing.Requirements
	SessionID      string
}

// Parse validates and extracts routing metadata. Unknown content types are
// rejected explicitly (never silently dropped).
func Parse(raw []byte) (Parsed, error) {
	var v struct {
		Model        *string         `json:"model"`
		MaxTokens    *int            `json:"max_tokens"`
		Messages     json.RawMessage `json:"messages"`
		Stream       *bool           `json:"stream"`
		Tools        json.RawMessage `json:"tools"`
		Thinking     json.RawMessage `json:"thinking"`
		OutputConfig json.RawMessage `json:"output_config"`
		System       json.RawMessage `json:"system"`
		ToolChoice   json.RawMessage `json:"tool_choice"`
		Temperature  *float64        `json:"temperature"`
		TopP         *float64        `json:"top_p"`
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
	if *v.MaxTokens <= 0 {
		return Parsed{}, fmt.Errorf("max_tokens must be greater than zero")
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawFields); err != nil {
		return Parsed{}, fmt.Errorf("malformed JSON: %w", err)
	}
	for _, field := range []string{"context_management", "service_tier", "inference_geo", "container", "mcp_servers", "cache_control", "stop_sequences", "top_k"} {
		if value, ok := rawFields[field]; ok && strings.TrimSpace(string(value)) != "null" {
			return Parsed{}, fmt.Errorf("%s is not supported by the OpenAI Responses bridge", field)
		}
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
	if err := validateBlocks(typed); err != nil {
		return Parsed{}, err
	}
	if err := validateOptions(typed); err != nil {
		return Parsed{}, err
	}
	if err := validateThinking(typed.Thinking); err != nil {
		return Parsed{}, err
	}
	p.Requirements = deriveRequirements(typed)
	return p, nil
}

func deriveRequirements(t MessageRequest) routing.Requirements {
	var r routing.Requirements
	if len(t.Tools) > 0 {
		r.Tools = true
	}
	if len(t.Thinking) > 0 && !isJSONNull(t.Thinking) {
		var th struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(t.Thinking, &th); err != nil || (th.Type != "" && th.Type != "disabled") {
			// Claude Code sends "adaptive" for model IDs it does not recognize,
			// including gateway aliases; it is still a reasoning requirement.
			r.Reasoning = true
		}
	}
	if len(t.OutputConfig) > 0 && !isJSONNull(t.OutputConfig) {
		var output struct {
			Effort string `json:"effort"`
		}
		if err := json.Unmarshal(t.OutputConfig, &output); err != nil || strings.TrimSpace(output.Effort) != "" {
			r.Reasoning = true
		}
	}
	for _, message := range t.Messages {
		content := strings.TrimSpace(string(message.Content))
		if strings.HasPrefix(content, "\"") {
			continue
		}
		var blocks []ContentBlock
		if json.Unmarshal(message.Content, &blocks) != nil {
			continue // validateBlocks reports malformed content.
		}
		for _, block := range blocks {
			if block.Type == "image" || block.Type == "document" || (block.Type == "tool_result" && nestedContentNeedsVision(block.Content)) {
				r.Vision = true
			}
		}
	}
	return r
}

func nestedContentNeedsVision(raw json.RawMessage) bool {
	var blocks []ContentBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return false
	}
	for _, block := range blocks {
		if block.Type == "image" || block.Type == "document" {
			return true
		}
	}
	return false
}

func validateBlocks(t MessageRequest) error {
	toolUses := map[string]bool{}
	toolResults := map[string]bool{}
	for i, m := range t.Messages {
		if len(m.OutputConfig) > 0 && !isJSONNull(m.OutputConfig) {
			return fmt.Errorf("messages[%d].output_config is not supported by the OpenAI Responses bridge", i)
		}
		if m.Role != "user" && m.Role != "assistant" && m.Role != "system" {
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
			case "text", "image", "document", "tool_use", "tool_result", "thinking", "redacted_thinking":
			case "":
				return fmt.Errorf("messages[%d].content[%d]: missing type", i, j)
			default:
				return fmt.Errorf("messages[%d].content[%d]: unknown content type %q", i, j, b.Type)
			}
			if (b.Type == "tool_use" || b.Type == "thinking" || b.Type == "redacted_thinking") && m.Role != "assistant" {
				return fmt.Errorf("messages[%d].content[%d]: %s blocks require assistant role", i, j, b.Type)
			}
			if (b.Type == "tool_result" || b.Type == "image" || b.Type == "document") && m.Role != "user" {
				return fmt.Errorf("messages[%d].content[%d]: %s blocks require user role", i, j, b.Type)
			}
			if b.Type == "tool_use" {
				if b.ID == "" || b.Name == "" {
					return fmt.Errorf("messages[%d].content[%d]: tool_use requires id and name", i, j)
				}
				var input map[string]json.RawMessage
				if len(b.Input) == 0 || json.Unmarshal(b.Input, &input) != nil || input == nil {
					return fmt.Errorf("messages[%d].content[%d]: tool_use input must be a JSON object", i, j)
				}
				if toolUses[b.ID] {
					return fmt.Errorf("messages[%d].content[%d]: duplicate tool_use id %q", i, j, b.ID)
				}
				toolUses[b.ID] = true
			}
			if b.Type == "tool_result" {
				if b.ToolUseID == "" {
					return fmt.Errorf("messages[%d].content[%d]: tool_result requires tool_use_id", i, j)
				}
				if !toolUses[b.ToolUseID] {
					return fmt.Errorf("messages[%d].content[%d]: tool_result references unknown tool_use_id %q", i, j, b.ToolUseID)
				}
				if toolResults[b.ToolUseID] {
					return fmt.Errorf("messages[%d].content[%d]: duplicate tool_result for %q", i, j, b.ToolUseID)
				}
				toolResults[b.ToolUseID] = true
				if err := validateToolResultContent(b.Content); err != nil {
					return fmt.Errorf("messages[%d].content[%d]: %w", i, j, err)
				}
			}
		}
	}
	// Validate system: string or block list.
	if len(t.System) > 0 && !isJSONNull(t.System) {
		s := strings.TrimSpace(string(t.System))
		if !strings.HasPrefix(s, "\"") && !strings.HasPrefix(s, "[") {
			return fmt.Errorf("system must be string or block list")
		}
		if strings.HasPrefix(s, "[") {
			var blocks []ContentBlock
			if err := json.Unmarshal(t.System, &blocks); err != nil {
				return fmt.Errorf("system must be string or block list")
			}
			for i, block := range blocks {
				if block.Type != "text" || block.Text == "" {
					return fmt.Errorf("system[%d]: only non-empty text blocks are supported", i)
				}
			}
		}
	}
	return nil
}

func validateToolResultContent(raw json.RawMessage) error {
	content := strings.TrimSpace(string(raw))
	if content == "" || content == "null" || strings.HasPrefix(content, `"`) {
		return nil
	}
	if !strings.HasPrefix(content, "[") {
		return fmt.Errorf("tool_result content must be a string or block list")
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return fmt.Errorf("tool_result content must be a string or block list")
	}
	for i, block := range blocks {
		switch block.Type {
		case "text", "image", "document":
		default:
			return fmt.Errorf("tool_result content[%d]: unsupported block type %q", i, block.Type)
		}
	}
	return nil
}

func validateOptions(t MessageRequest) error {
	toolNames := make(map[string]bool, len(t.Tools))
	for i, tool := range t.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			return fmt.Errorf("tools[%d]: name is required", i)
		}
		if tool.Type != "" && tool.Type != "custom" {
			return fmt.Errorf("tools[%d]: Anthropic server tool type %q is unsupported", i, tool.Type)
		}
		if tool.DeferLoading != nil && *tool.DeferLoading {
			return fmt.Errorf("tools[%d]: defer_loading is unsupported by the Responses bridge", i)
		}
		if toolNames[tool.Name] {
			return fmt.Errorf("tools[%d]: duplicate name %q", i, tool.Name)
		}
		toolNames[tool.Name] = true
		var schema map[string]json.RawMessage
		if len(tool.InputSchema) == 0 || json.Unmarshal(tool.InputSchema, &schema) != nil || schema == nil {
			return fmt.Errorf("tools[%d]: input_schema must be a JSON object", i)
		}
	}
	if err := validateToolChoice(t.ToolChoice, toolNames); err != nil {
		return err
	}
	if len(t.Metadata) > 0 && !isJSONNull(t.Metadata) {
		var metadata struct {
			UserID *string `json:"user_id"`
		}
		var raw map[string]json.RawMessage
		if json.Unmarshal(t.Metadata, &raw) != nil || raw == nil {
			return fmt.Errorf("metadata must be an object")
		}
		for key := range raw {
			if key != "user_id" {
				return fmt.Errorf("metadata.%s is unsupported", key)
			}
		}
		if value, ok := raw["user_id"]; ok {
			if json.Unmarshal(value, &metadata.UserID) != nil || (metadata.UserID != nil && utf8.RuneCountInString(*metadata.UserID) > 512) {
				return fmt.Errorf("metadata.user_id must be a string of at most 512 characters")
			}
		}
	}
	if t.Temperature != nil && (*t.Temperature < 0 || *t.Temperature > 1) {
		return fmt.Errorf("temperature must be between 0 and 1")
	}
	if t.TopP != nil && (*t.TopP < 0 || *t.TopP > 1) {
		return fmt.Errorf("top_p must be between 0 and 1")
	}
	return validateOutputConfig(t.OutputConfig)
}

func validateThinking(raw json.RawMessage) error {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil
	}
	var thinking struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &thinking) != nil {
		return fmt.Errorf("thinking must be an object")
	}
	switch thinking.Type {
	case "disabled", "adaptive":
		return nil
	case "enabled":
		return fmt.Errorf("thinking.type=enabled uses Anthropic token budgets that cannot be represented by the Responses upstream; use output_config.effort")
	default:
		return fmt.Errorf("unsupported thinking.type %q", thinking.Type)
	}
}

func validateToolChoice(raw json.RawMessage, tools map[string]bool) error {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, `"`) {
		var choice string
		if json.Unmarshal(raw, &choice) != nil || (choice != "auto" && choice != "none") {
			return fmt.Errorf("tool_choice must be auto, none, or a supported object")
		}
		return nil
	}
	var choice struct {
		Type                   string `json:"type"`
		Name                   string `json:"name"`
		DisableParallelToolUse *bool  `json:"disable_parallel_tool_use"`
	}
	if json.Unmarshal(raw, &choice) != nil {
		return fmt.Errorf("tool_choice must be an object")
	}
	switch choice.Type {
	case "auto", "any", "none":
		if choice.Name != "" {
			return fmt.Errorf("tool_choice %q must not include name", choice.Type)
		}
	case "tool":
		if choice.Name == "" || !tools[choice.Name] {
			return fmt.Errorf("tool_choice tool must name a declared tool")
		}
	default:
		return fmt.Errorf("unsupported tool_choice type %q", choice.Type)
	}
	return nil
}

func validateOutputConfig(raw json.RawMessage) error {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil
	}
	var config struct {
		Effort *string `json:"effort"`
		Format *struct {
			Type   string          `json:"type"`
			Schema json.RawMessage `json:"schema"`
		} `json:"format"`
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return fmt.Errorf("output_config must be an object")
	}
	for field := range object {
		if field != "effort" && field != "format" {
			return fmt.Errorf("output_config.%s is not supported by the Responses bridge", field)
		}
	}
	if json.Unmarshal(raw, &config) != nil {
		return fmt.Errorf("output_config is malformed")
	}
	if config.Effort != nil {
		switch *config.Effort {
		case "low", "medium", "high", "xhigh", "max":
		default:
			return fmt.Errorf("output_config.effort %q is unsupported", *config.Effort)
		}
	}
	if config.Format != nil {
		if config.Format.Type != "json_schema" {
			return fmt.Errorf("output_config.format.type %q is unsupported", config.Format.Type)
		}
		var schema map[string]json.RawMessage
		if len(config.Format.Schema) == 0 || json.Unmarshal(config.Format.Schema, &schema) != nil || schema == nil {
			return fmt.Errorf("output_config.format.schema must be a JSON object")
		}
	}
	return nil
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}
