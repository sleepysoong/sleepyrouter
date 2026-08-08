package srv

import (
	"testing"

	"github.com/sleepysoong/sleepyrouter/internal/protocol"
)

func TestExtractTextContent_String(t *testing.T) {
	got, err := protocol.ExtractTextContent("hello")
	if err != nil || got != "hello" {
		t.Fatalf("expected 'hello', got %q (err: %v)", got, err)
	}
}

func TestExtractTextContent_StringArray(t *testing.T) {
	got, err := protocol.ExtractTextContent([]any{"a", "b"})
	if err != nil || got != "a\nb" {
		t.Fatalf("expected 'a\\nb', got %q (err: %v)", got, err)
	}
}

func TestExtractTextContent_Blocks(t *testing.T) {
	got, err := protocol.ExtractTextContent([]any{
		map[string]any{"type": "text", "text": "hello"},
		map[string]any{"type": "text", "text": "world"},
	})
	if err != nil || got != "hello\nworld" {
		t.Fatalf("expected 'hello\\nworld', got %q (err: %v)", got, err)
	}
}

func TestExtractTextContent_RejectsImage(t *testing.T) {
	if _, err := protocol.ExtractTextContent([]any{map[string]any{"type": "image", "source": map[string]any{}}}); err == nil {
		t.Fatal("expected error for unsupported block")
	}
}

func TestExtractTextContent_RejectsNonTextBlocks(t *testing.T) {
	if _, err := protocol.ExtractTextContent([]any{map[string]any{"type": "image", "source": map[string]any{}}}); err == nil {
		t.Fatal("expected error for unsupported block")
	}
}

func TestExtractTextContent_WithThinkingBlocks(t *testing.T) {
	got, err := protocol.ExtractTextContent([]any{
		map[string]any{"type": "thinking", "thinking": "analyzing request..."},
		map[string]any{"type": "redacted_thinking", "data": "abc"},
		map[string]any{"type": "text", "text": "final answer"},
	})
	if err != nil || got != "analyzing request...\nfinal answer" {
		t.Fatalf("expected 'analyzing request...\\nfinal answer', got %q (err: %v)", got, err)
	}
}
