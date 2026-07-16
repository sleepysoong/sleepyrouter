package utils

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var tokenCountPattern = regexp.MustCompile(`(?i)([0-9][0-9,]*(?:\.[0-9]+)?)\s*(k|m|thousand|million)?`)

var contextKeys = []string{
	"context_length", "max_context_length", "contextLength", "maxContextLength",
	"context_window", "contextWindow", "context_size", "contextSize", "max_model_len",
	"maxModelLen", "input_context_length", "inputContextLength", "max_input_tokens", "maxInputTokens",
}

var contextContainers = []string{"metadata", "capabilities", "model_config", "modelConfig", "config", "limits"}

func IntPointer(value int) *int { return &value }

func ParseTokenCount(value any) *int {
	switch number := value.(type) {
	case float64:
		if !math.IsNaN(number) && !math.IsInf(number, 0) && number > 0 {
			return IntPointer(int(math.Round(number)))
		}
		return nil
	case float32:
		if !math.IsNaN(float64(number)) && !math.IsInf(float64(number), 0) && number > 0 {
			return IntPointer(int(math.Round(float64(number))))
		}
		return nil
	case int:
		if number > 0 {
			return IntPointer(number)
		}
		return nil
	case int64:
		if number > 0 {
			return IntPointer(int(number))
		}
		return nil
	case jsonNumber:
		return parseTokenCountString(string(number), false)
	case string:
		return parseTokenCountString(number, false)
	default:
		return nil
	}
}

// jsonNumber keeps this package independent from callers that decode numbers
// with json.Decoder.UseNumber while retaining the TypeScript numeric behavior.
type jsonNumber string

func parseTokenCountString(value string, rejectSmallUnitless bool) *int {
	match := tokenCountPattern.FindStringSubmatch(value)
	if len(match) == 0 {
		return nil
	}
	return parseTokenCountParts(match[1], match[2], rejectSmallUnitless)
}

func parseTokenCountParts(rawNumber, rawUnit string, rejectSmallUnitless bool) *int {
	number, err := strconv.ParseFloat(strings.ReplaceAll(rawNumber, ",", ""), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number <= 0 {
		return nil
	}
	unit := strings.ToLower(rawUnit)
	if unit == "" && rejectSmallUnitless && number < 1024 {
		return nil
	}
	multiplier := 1.0
	if unit == "m" || unit == "million" {
		multiplier = 1_000_000
	} else if unit == "k" || unit == "thousand" {
		multiplier = 1_000
	}
	return IntPointer(int(math.Round(number * multiplier)))
}

func ExtractContextLengthFromRecord(record map[string]any) *int {
	for _, key := range contextKeys {
		if contextLength := ParseTokenCount(record[key]); contextLength != nil {
			return contextLength
		}
	}
	for _, key := range contextContainers {
		container, ok := asRecord(record[key])
		if !ok {
			continue
		}
		for _, contextKey := range contextKeys {
			if contextLength := ParseTokenCount(container[contextKey]); contextLength != nil {
				return contextLength
			}
		}
	}
	return nil
}

func NormalizeMetadataText(text string) string {
	text = strings.ReplaceAll(text, `\n`, "\n")
	text = strings.ReplaceAll(text, `\u003e`, ">")
	text = strings.ReplaceAll(text, `\u003E`, ">")
	text = strings.ReplaceAll(text, `\u003c`, "<")
	text = strings.ReplaceAll(text, `\u003C`, "<")
	text = strings.ReplaceAll(text, `\"`, `"`)
	tagPattern := regexp.MustCompile(`<[^>]+>`)
	spacePattern := regexp.MustCompile(`\s+`)
	text = tagPattern.ReplaceAllString(text, " ")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	return spacePattern.ReplaceAllString(text, " ")
}

type contextCandidate struct {
	contextLength int
	score         int
	index         int
}

type contextPattern struct {
	pattern *regexp.Regexp
	score   int
}

var contextTextPatterns = []contextPattern{
	{regexp.MustCompile(`(?i)(?:maximum|max|input)?\s*context\s+length(?:\s*\([^)]+\))?[^0-9]{0,80}?(?:up to|of|is|:|\|)?\s*([0-9][0-9,]*(?:\.[0-9]+)?)\s*(k|m|thousand|million)?(?:\s*tokens?)?`), 100},
	{regexp.MustCompile(`(?i)context\s+(?:window|size)[^0-9]{0,80}?(?:up to|of|is|:|\|)?\s*([0-9][0-9,]*(?:\.[0-9]+)?)\s*(k|m|thousand|million)?(?:\s*tokens?)?`), 95},
	{regexp.MustCompile(`(?i)([0-9][0-9,]*(?:\.[0-9]+)?)\s*(k|m|thousand|million)\s*[- ]?\s*tokens?\s+context\s+(?:window|length|size)s?`), 85},
	{regexp.MustCompile(`(?i)([0-9][0-9,]*(?:\.[0-9]+)?)\s*(k|m|thousand|million)\s*tokens?\s+(?:of|for)\s+context\b`), 80},
	{regexp.MustCompile(`(?i)([0-9][0-9,]*(?:\.[0-9]+)?)\s*(k|m|thousand|million)\s+context\b`), 75},
}

func ParseContextLengthFromText(text string) *int {
	normalized := NormalizeMetadataText(text)
	candidates := []contextCandidate{}
	for _, pattern := range contextTextPatterns {
		for _, match := range pattern.pattern.FindAllStringSubmatchIndex(normalized, -1) {
			if len(match) < 6 || match[2] < 0 {
				continue
			}
			rawNumber := normalized[match[2]:match[3]]
			rawUnit := ""
			if match[4] >= 0 {
				rawUnit = normalized[match[4]:match[5]]
			}
			if contextLength := parseTokenCountParts(rawNumber, rawUnit, true); contextLength != nil {
				candidates = append(candidates, contextCandidate{contextLength: *contextLength, score: pattern.score, index: match[0]})
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].contextLength != candidates[j].contextLength {
			return candidates[i].contextLength > candidates[j].contextLength
		}
		return candidates[i].index < candidates[j].index
	})
	return IntPointer(candidates[0].contextLength)
}

func asRecord(value any) (map[string]any, bool) {
	record, ok := value.(map[string]any)
	return record, ok
}

func StringFromUnknown(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func StringSliceFromUnknown(value any) []string {
	values, ok := value.([]any)
	if !ok {
		if stringsValue, ok := value.([]string); ok {
			return append([]string(nil), stringsValue...)
		}
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func UnknownString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}
