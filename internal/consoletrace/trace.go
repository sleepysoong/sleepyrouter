// Package consoletrace prints an ephemeral, human-readable view of inference
// requests and provider-supplied reasoning deltas to stdout.
package consoletrace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	maxRequestInput = 1 << 20
	maxRequestView  = 32 << 10
)

// Trace writes directly to the process console. It has no file or database
// backing; callers may still capture stdout outside this process.
type Trace struct {
	mu     sync.Mutex
	out    io.Writer
	color  bool
	active map[string]string
}

// New creates a console trace. ANSI color is enabled only for character-device
// stdout and can be disabled with NO_COLOR.
func New(out io.Writer) *Trace {
	if out == nil {
		return nil
	}
	t := &Trace{out: out, active: make(map[string]string)}
	if f, ok := out.(*os.File); ok && os.Getenv("NO_COLOR") == "" {
		if info, err := f.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			t.color = true
		}
	}
	return t
}

// Request prints a bounded, pretty-printed request body after redacting
// credential-shaped JSON fields.
func (t *Trace) Request(requestID, protocol, model string, raw []byte, secrets []string) {
	if t == nil {
		return
	}
	body, note := requestPreview(raw, secrets)
	t.mu.Lock()
	defer t.mu.Unlock()
	var block strings.Builder
	fmt.Fprintf(&block, "%s╭── REQUEST · %s · %s ──╮%s\n", t.cyan(), cleanLabel(protocol), cleanLabel(model), t.reset())
	fmt.Fprintf(&block, "│ id: %s\n│ body:%s\n", cleanLabel(requestID), note)
	for _, line := range strings.Split(strings.TrimSuffix(body, "\n"), "\n") {
		fmt.Fprintf(&block, "│ %s\n", line)
	}
	fmt.Fprintf(&block, "%s╰────────────────────────────────────────╯%s\n", t.cyan(), t.reset())
	_, _ = io.WriteString(t.out, block.String())
}

// ReasoningDelta prints one provider-supplied text delta as it arrives. It
// does not alter or persist the stream sent to the API client.
func (t *Trace) ReasoningDelta(requestID, model, delta string, secrets []string) {
	if t == nil || delta == "" {
		return
	}
	delta = redactText(delta, secrets)
	t.mu.Lock()
	defer t.mu.Unlock()
	_, started := t.active[requestID]
	var block strings.Builder
	if !started {
		t.active[requestID] = model
		fmt.Fprintf(&block, "%s╭── LIVE REASONING · %s · %s ──╮%s\n", t.magenta(), cleanLabel(requestID), cleanLabel(model), t.reset())
	}
	for _, line := range strings.Split(strings.TrimSuffix(clean(delta), "\n"), "\n") {
		fmt.Fprintf(&block, "│ %s\n", line)
	}
	_, _ = io.WriteString(t.out, block.String())
}

// EndReasoning closes the console section for a finished stream attempt.
func (t *Trace) EndReasoning(requestID, model string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	startedModel, ok := t.active[requestID]
	if !ok {
		return
	}
	delete(t.active, requestID)
	if model == "" {
		model = startedModel
	}
	fmt.Fprintf(t.out, "%s╰── END REASONING · %s · %s ──╯%s\n", t.magenta(), cleanLabel(requestID), cleanLabel(model), t.reset())
}

func requestPreview(raw []byte, secrets []string) (string, string) {
	if len(raw) > maxRequestInput {
		return fmt.Sprintf("[body omitted: %d bytes exceeds the %d-byte console parsing limit]", len(raw), maxRequestInput), " (credentials masked; large body omitted)"
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return "[body omitted: invalid JSON]", " (invalid JSON)"
	}
	value = redact(value, secrets)
	formatted, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "[body omitted: could not format JSON]", ""
	}
	if len(formatted) > maxRequestView {
		end := maxRequestView
		for end > 0 && !utf8.Valid(formatted[:end]) {
			end--
		}
		return string(formatted[:end]) + "\n… [preview truncated]", " (sensitive fields masked; preview limited to 32 KiB)"
	}
	return string(formatted), " (sensitive fields masked)"
}

func redact(value any, secrets []string) any {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if sensitiveKey(key) {
				v[key] = "[redacted]"
				continue
			}
			v[key] = redact(child, secrets)
		}
		return v
	case []any:
		for i, child := range v {
			v[i] = redact(child, secrets)
		}
		return v
	case string:
		return redactText(v, secrets)
	default:
		return value
	}
}

func redactText(value string, secrets []string) string {
	for _, secret := range secrets {
		if len(secret) >= 4 {
			value = strings.ReplaceAll(value, secret, "[redacted]")
		}
	}
	return value
}

func sensitiveKey(key string) bool {
	var normalized strings.Builder
	for _, r := range strings.ToLower(key) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			normalized.WriteRune(r)
		}
	}
	k := normalized.String()
	if strings.Contains(k, "apikey") || strings.Contains(k, "secret") || strings.Contains(k, "privatekey") || strings.Contains(k, "credential") || strings.HasSuffix(k, "token") || k == "tokenvalue" || k == "tokenstring" {
		return true
	}
	switch k {
	case "auth", "authorization", "proxyauthorization", "password", "passwd":
		return true
	default:
		return strings.HasSuffix(k, "password")
	}
}

func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, strings.ToValidUTF8(s, "�"))
}

func cleanLabel(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, strings.ToValidUTF8(s, "�"))
}

func (t *Trace) cyan() string {
	if t.color {
		return "\x1b[36m"
	}
	return ""
}

func (t *Trace) magenta() string {
	if t.color {
		return "\x1b[35m"
	}
	return ""
}

func (t *Trace) reset() string {
	if t.color {
		return "\x1b[0m"
	}
	return ""
}
