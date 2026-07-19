package srv

import (
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/handler"
	"github.com/sleepysoong/sleepyrouter/internal/types"
	"github.com/sleepysoong/sleepyrouter/internal/utils"
)

// ServerOptions configures a SleepyRouter http.Server. Each field has a
// sensible default so callers only set what they need.
type ServerOptions struct {
	Store         *cfg.ConfigStore
	FetchImpl     types.HTTPDoer
	Env           utils.Environment
	RequestLogger func(handler.ServerLogEvent)
	StartTime     time.Time
}
