package utils

import (
	"testing"
)

func TestParseTokenCount_Float64(t *testing.T) {
	if got := ParseTokenCount(float64(1000)); got == nil || *got != 1000 {
		t.Fatalf("got %v", got)
	}
}

func TestParseTokenCount_Invalid(t *testing.T) {
	if got := ParseTokenCount(-1); got != nil {
		t.Fatalf("expected nil, got %d", *got)
	}
}

func TestParseTokenCount_Nil(t *testing.T) {
	if got := ParseTokenCount(nil); got != nil {
		t.Fatalf("expected nil")
	}
}

func TestParseTokenCount_StringK(t *testing.T) {
	if got := ParseTokenCount("128k"); got == nil || *got != 128000 {
		t.Fatalf("got %v", got)
	}
}

func TestParseTokenCount_StringM(t *testing.T) {
	if got := ParseTokenCount("1M"); got == nil || *got != 1000000 {
		t.Fatalf("got %v", got)
	}
}

func TestParseTokenCount_MissingUnit(t *testing.T) {
	if got := ParseTokenCount("unrelated"); got != nil {
		t.Fatalf("expected nil, got %d", *got)
	}
}

func TestExtractContextLengthFromRecord_Direct(t *testing.T) {
	expected := 1000
	catalog := map[string]any{"context_length": float64(expected)}
	if got := ExtractContextLengthFromRecord(catalog); got == nil || *got != expected {
		t.Fatalf("got %v", got)
	}
}

func TestExtractContextLengthFromRecord_Nested(t *testing.T) {
	expected := 2000
	catalog := map[string]any{"capabilities": map[string]any{"context_length": float64(expected)}}
	if got := ExtractContextLengthFromRecord(catalog); got == nil || *got != expected {
		t.Fatalf("got %v", got)
	}
}

func TestNormalizeMetadataText(t *testing.T) {
	result := NormalizeMetadataText("hello\\u003cworld\\nnext")
	if result != "hello<world next" {
		t.Fatalf("got %q", result)
	}
}
