package sleepyrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func httpClient(client HTTPDoer) HTTPDoer {
	if client != nil {
		return client
	}
	return http.DefaultClient
}

func marshalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

func jsonRequest(ctx context.Context, method, url string, headers map[string]string, body any) (*http.Request, error) {
	data, err := marshalJSON(body)
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

func getRequest(ctx context.Context, url string, headers map[string]string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	return req, nil
}

func responseJSON(response *http.Response) (map[string]any, error) {
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

func responseText(response *http.Response) (string, error) {
	if response == nil || response.Body == nil {
		return "", nil
	}
	data, err := io.ReadAll(response.Body)
	return string(data), err
}

func statusText(response *http.Response) string {
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

func isOK(response *http.Response) bool {
	return response != nil && response.StatusCode >= 200 && response.StatusCode < 300
}

func cloneObject(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value)+1)
	for key, item := range value {
		clone[key] = item
	}
	return clone
}

func objectValue(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	return object, ok
}

func arrayValue(value any) ([]any, bool) {
	array, ok := value.([]any)
	return array, ok
}

func boolValue(value any) bool {
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

func numberValue(value any) *int {
	if number, ok := value.(float64); ok && number >= 0 && number == float64(int(number)) {
		return intPointer(int(number))
	}
	if number, ok := value.(int); ok && number >= 0 {
		return intPointer(number)
	}
	return nil
}
