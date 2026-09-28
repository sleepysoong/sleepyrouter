package server

import (
	"context"
	"sync/atomic"
	"time"
)

const firstMeaningfulOutputTimeout = 10 * time.Second

type firstOutputDeadlineContextKey struct{}

// candidateStreamContext starts the first-output deadline before the upstream
// request is sent. The timer is stopped once DoStream returns so an active
// response stream can continue after its first-output window.
func candidateStreamContext(parent context.Context, started time.Time) (context.Context, context.CancelFunc, func(), func() bool) {
	ctx, cancel := context.WithCancel(parent)
	deadline := started.Add(firstMeaningfulOutputTimeout)
	ctx = context.WithValue(ctx, firstOutputDeadlineContextKey{}, deadline)

	var expired atomic.Bool
	finished := make(chan struct{})
	timer := time.AfterFunc(time.Until(deadline), func() {
		expired.Store(true)
		cancel()
		close(finished)
	})
	stopTimer := func() {
		if !timer.Stop() {
			<-finished
		}
	}
	return ctx, cancel, stopTimer, expired.Load
}

func firstOutputDeadline(ctx context.Context) time.Time {
	deadline, _ := ctx.Value(firstOutputDeadlineContextKey{}).(time.Time)
	return deadline
}

func firstOutputTimedOut(deadline time.Time) bool {
	return !deadline.IsZero() && !time.Now().Before(deadline)
}
