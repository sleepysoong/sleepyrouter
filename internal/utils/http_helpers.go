package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sleepysoong/sleepyrouter/internal/types"
)

func HTTPClient(client types.HTTPDoer) types.HTTPDoer {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

func MarshalJSONHelper(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func JSONRequest(ctx context.Context, method, url string, headers map[string]string, body any) (*http.Request, error) {
	data, err := MarshalJSONHelper(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return req, nil
}

func GetRequest(ctx context.Context, url string, headers map[string]string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return req, nil
}

func ResponseJSON(response *http.Response) (map[string]any, error) {
	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("업스트림 응답 본문이 없어요")
	}
	var body map[string]any
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(&body); err != nil {
		return nil, err
	}
	return body, nil
}

func StatusText(response *http.Response) string {
	if response == nil {
		return ""
	}
	if response.Status != "" {
		parts := strings.SplitN(response.Status, " ", 2)
		if len(parts) == 2 {
			return parts[1]
		}
	}
	return http.StatusText(response.StatusCode)
}

func IsOK(response *http.Response) bool {
	return response != nil && response.StatusCode >= 200 && response.StatusCode < 300
}

func CloneObject(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value)+1)
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func ObjectValue(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	return object, ok
}

func ArrayValue(value any) ([]any, bool) {
	array, ok := value.([]any)
	return array, ok
}

func BoolValue(value any) bool {
	switch value := value.(type) {
	case nil:
		return false
	case bool:
		return value
	case float64:
		return value != 0
	case string:
		return value != ""
	default:
		return true
	}
}

func NumberValue(value any) *int {
	if number, ok := value.(float64); ok && number >= 0 && number == float64(int(number)) {
		return IntPointer(int(number))
	}
	if number, ok := value.(int); ok && number >= 0 {
		return IntPointer(number)
	}
	return nil
}

type HTTPClientFunc func(*http.Request) (*http.Response, error)

func (f HTTPClientFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

// IntPointer returns a pointer to v.
func IntPointer(v int) *int { return &v }

// StringFromUnknown returns value as a string if it is one, otherwise "".
func StringFromUnknown(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// UnknownString returns value as a string via Sprint if not a string.
func UnknownString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

func ReadBody(r *http.Request) (map[string]any, error) {
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
		return nil, fmt.Errorf("요청 본문을 파싱할 수 없어요. 유효한 JSON을 보내주세요. (%d바이트 수신)", len(text))
	}
	return body, nil
}

