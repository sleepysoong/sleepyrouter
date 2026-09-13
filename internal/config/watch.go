package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch starts watching home dir for config.toml/.env changes.
// onReload is called debounced; it should reload+validate+swap.
// Returns a stop func.
func Watch(home string, log *slog.Logger, onReload func()) (func(), error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := w.Add(home); err != nil {
		w.Close()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer w.Close()
		var timer *time.Timer
		var ch <-chan time.Time
		trigger := func() {
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(300 * time.Millisecond)
			ch = timer.C
		}
		for {
			select {
			case <-done:
				if timer != nil {
					timer.Stop()
				}
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				base := filepath.Base(ev.Name)
				if base != "config.toml" && base != ".env" {
					continue
				}
				trigger()
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				if log != nil {
					log.Error("config watcher error", "error", err)
				}
			case <-ch:
				ch = nil
				func() {
					defer func() {
						if r := recover(); r != nil && log != nil {
							log.Error("config reload panic", "panic", r)
						}
					}()
					onReload()
				}()
			}
		}
	}()
	stop := func() {
		func() {
			defer func() { _ = recover() }()
			close(done)
		}()
		_ = os.Stdout.Sync()
	}
	return stop, nil
}
