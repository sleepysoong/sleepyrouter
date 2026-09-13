// Package upstream executes OpenAI-compatible requests via the official SDK.
// It owns SDK types; routing must not import this package's SDK surface.
package upstream

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/sleepysoong/sleepyrouter/internal/config"
)

var sharedTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	TLSHandshakeTimeout:   10 * time.Second,
	MaxIdleConns:          100,
	MaxIdleConnsPerHost:   20,
	IdleConnTimeout:       90 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
}

// SharedHTTPClient reuses connections; no overall Timeout so streams aren't cut.
func SharedHTTPClient() *http.Client {
	return &http.Client{Transport: sharedTransport, Timeout: 0}
}

// NewClient builds a per-provider SDK client. SDK retry is always 0;
// sleepyrouter failover is authoritative (Invariant 5).
func NewClient(p *config.RuntimeProvider) openai.Client {
	opts := []option.RequestOption{
		option.WithBaseURL(p.BaseURL),
		option.WithAPIKey(p.APIKey),
		option.WithHTTPClient(SharedHTTPClient()),
		option.WithMaxRetries(0),
	}
	for k, v := range p.Headers {
		opts = append(opts, option.WithHeader(k, v))
	}
	return openai.NewClient(opts...)
}
