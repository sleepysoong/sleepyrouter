package anthropic

import "encoding/json"

// These types describe only the Messages wire forms this gateway emits.
// They are intentionally distinct from SDK request Param types.
type wireUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type wireContentBlock struct {
	Type  string          `json:"type"`
	Text  *string         `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type wireMessage struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Content      []wireContentBlock `json:"content"`
	Model        string             `json:"model"`
	StopReason   *string            `json:"stop_reason"`
	StopSequence *string            `json:"stop_sequence"`
	Usage        wireUsage          `json:"usage"`
}

type wireMessageStart struct {
	Type    string      `json:"type"`
	Message wireMessage `json:"message"`
}

type wireBlockStart struct {
	Type         string           `json:"type"`
	Index        int              `json:"index"`
	ContentBlock wireContentBlock `json:"content_block"`
}

type wireTextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type wireInputJSONDelta struct {
	Type        string `json:"type"`
	PartialJSON string `json:"partial_json"`
}

type wireBlockDelta struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta any    `json:"delta"`
}

type wireBlockStop struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

type wireStopDelta struct {
	StopReason   string  `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

type wireDeltaUsage struct {
	OutputTokens int64 `json:"output_tokens"`
}

type wireMessageDelta struct {
	Type  string         `json:"type"`
	Delta wireStopDelta  `json:"delta"`
	Usage wireDeltaUsage `json:"usage"`
}

type wireMessageStop struct {
	Type string `json:"type"`
}

type wireErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type wireErrorEvent struct {
	Type  string        `json:"type"`
	Error wireErrorBody `json:"error"`
}

func wireString(s string) *string { return &s }
