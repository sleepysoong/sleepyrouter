package srv

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/handler"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

func CreateSleepyRouterServer(options ServerOptions) *http.Server {
	store := options.Store
	if store == nil {
		store = cfg.NewConfigStore("")
	}
	env := options.Env
	if env == nil {
		env = utils.CurrentEnvironment()
	}
	client := options.FetchImpl
	requestLogger := options.RequestLogger
	startTime := options.StartTime
	if startTime.IsZero() {
		startTime = time.Now()
	}

	mux := http.NewServeMux()
	registerRoutes(mux, routeDeps{store: store, env: env, client: client, requestLogger: requestLogger, startTime: startTime})

	nextID := new(int64)
	handler := withObservation(mux, nextID, requestLogger, startTime)
	return &http.Server{
		Handler:     handler,
		ReadTimeout: 60 * time.Second,
		IdleTimeout: 120 * time.Second,
	}
}

// withObservation wraps mux so that every request gets a fresh HandlerState,
// a logRequest/logResponse pair, a responseRecorder (when logging is on),
// and a deferred panic recover. Route handlers retrieve the state through
// r.Context().
func withObservation(mux *http.ServeMux, nextID *int64, requestLogger func(handler.ServerLogEvent), startTime time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := int(atomic.AddInt64(nextID, 1))
		startedAt := time.Now()

		st := &handler.HandlerState{
			RequestID:     id,
			RequestMethod: r.Method,
			RequestPath:   r.URL.Path,
		}

		if requestLogger != nil {
			requestLogger(handler.ServerLogEvent{
				Type:   "request",
				ID:     id,
				Method: r.Method,
				Path:   r.URL.Path,
			})
			if _, ok := w.(*responseRecorder); !ok {
				w = newResponseRecorder(w)
			}
		}
		defer func() {
			if requestLogger != nil {
				statusCode := 500
				if writer, ok := w.(*responseRecorder); ok {
					statusCode = writer.statusCode
				}
				requestLogger(handler.ServerLogEvent{
					Type:           "response",
					ID:             id,
					Method:         r.Method,
					Path:           r.URL.Path,
					StatusCode:     statusCode,
					DurationMs:     int(time.Since(startedAt).Milliseconds()),
					RequestedModel: st.RequestedModel,
					ModelID:        st.RoutedModel,
					RouteReason:    st.RouteReason,
					Stream:         st.Stream,
					InputTokens:    st.LastInputTokens,
					OutputTokens:   st.LastOutputTokens,
					Error:          st.LastError,
					Group:          st.LogGroup,
					CandidateCount: st.LogCandidateCount,
					TriedCount:     st.LogTriedCount,
				})
			}
		}()
		defer func() {
			if err := recover(); err != nil {
				statusCode := 500
				if he, ok := err.(*handler.HTTPError); ok {
					statusCode = he.StatusCode
				}
				msg := fmt.Sprint(err)
				slog.Error("panic recovered", "method", r.Method, "path", r.URL.Path, "error", msg)
				handler.WriteJSONError(w, statusCode, msg, map[string]any{"request": r.Method + " " + r.URL.String()})
			}
		}()

		mux.ServeHTTP(w, r.WithContext(withState(r.Context(), st)))
	})
}

func Listen(server *http.Server, port int) (int, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, err
	}
	go func() { _ = server.Serve(ln) }()
	if tcpAddr, ok := ln.Addr().(*net.TCPAddr); ok {
		// ponytail: constants defined in RFCs
		return tcpAddr.Port, nil
	}
	return 0, fmt.Errorf("listen address is not a TCP address: %v", ln.Addr())
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	wrote      bool
}

func newResponseRecorder(w http.ResponseWriter) *responseRecorder {
	return &responseRecorder{ResponseWriter: w, statusCode: 200}
}

func (r *responseRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.statusCode = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
