package srv

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	data, _ := utils.MarshalJSONHelper(body)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(data)
}

// writeJSONError writes the standard upstream-style error envelope:
// `{"error": {"message": message}}`, optionally extended with extra keys
// merged into the inner error object (e.g. "details", "type", "request").
func writeJSONError(w http.ResponseWriter, status int, message string, extras ...map[string]any) {
	inner := map[string]any{"message": message}
	for _, e := range extras {
		for k, v := range e {
			inner[k] = v
		}
	}
	writeJSON(w, status, map[string]any{"error": inner})
}

type httpError struct {
	StatusCode int
	Message    string
}

func (e *httpError) Error() string { return e.Message }

func readBody(r *http.Request) (map[string]any, error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	text := string(data)
	if text == "" {
		return map[string]any{}, nil
	}
	var body map[string]any
	if json.Unmarshal(data, &body) != nil {
		return nil, &httpError{StatusCode: 400, Message: fmt.Sprintf("요청 본문을 파싱할 수 없어요. 유효한 JSON을 보내주세요. (%d바이트 수신)", len(text))}
	}
	return body, nil
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

func errorString(err any) string {
	switch e := err.(type) {
	case error:
		return e.Error()
	case string:
		return e
	default:
		return fmt.Sprint(e)
	}
}
