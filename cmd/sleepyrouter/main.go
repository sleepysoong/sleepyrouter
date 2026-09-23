// Command sleepyrouter is the localhost LLM routing gateway.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/buildinfo"
	"github.com/sleepysoong/sleepyrouter/internal/config"
	"github.com/sleepysoong/sleepyrouter/internal/logging"
	"github.com/sleepysoong/sleepyrouter/internal/provider"
	"github.com/sleepysoong/sleepyrouter/internal/server"
	"github.com/sleepysoong/sleepyrouter/internal/state"
	"github.com/sleepysoong/sleepyrouter/internal/usage"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve", "start":
		os.Exit(runServe(parseServeArgs(os.Args[2:])))
	case "validate":
		os.Exit(runValidate())
	case "doctor":
		os.Exit(runDoctor())
	case "models":
		os.Exit(runModels())
	case "usage":
		os.Exit(runUsage(os.Args[2:]))
	case "version", "--version", "-v":
		fmt.Printf("sleepyrouter %s (%s %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		printHelp()
		os.Exit(2)
	}
}

func printHelp() {
	fmt.Println(`sleepyrouter - localhost LLM routing gateway

Usage:
  sleepyrouter serve [--host 127.0.0.1] [--port 4567]
  sleepyrouter validate
  sleepyrouter doctor
  sleepyrouter models
  sleepyrouter usage [--today] [--week] [--date YYYYMMDD] [--model ID]
  sleepyrouter version`)
}

type serveOpts struct {
	host string
	port int
}

func parseServeArgs(args []string) serveOpts {
	o := serveOpts{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--host":
			if i+1 < len(args) {
				o.host = args[i+1]
				i++
			}
		case "--port":
			if i+1 < len(args) {
				var p int
				_, _ = fmt.Sscanf(args[i+1], "%d", &p)
				o.port = p
				i++
			}
		}
	}
	return o
}

func loadActive() (home string, cfg config.Config, dotenv map[string]string, log *slog.Logger) {
	home = config.ResolveHome()
	_ = os.MkdirAll(home, 0o755)
	dotenv = config.LoadDotenv(config.EnvPath(home))
	// Check .env perms.
	if fi, err := os.Stat(config.EnvPath(home)); err == nil {
		if fi.Mode().Perm()&0o077 != 0 && os.Getenv("OS") == "" {
			fmt.Fprintf(os.Stderr, "warning: %s is %o, recommend 0600\n", config.EnvPath(home), fi.Mode().Perm())
		}
	}
	data, err := os.ReadFile(config.ConfigPath(home))
	if err != nil {
		fmt.Fprintf(os.Stderr, "config not found: %s\n", config.ConfigPath(home))
		fmt.Fprintf(os.Stderr, "create one from config.example.toml, e.g.:\n  mkdir -p %s && cp config.example.toml %s\n", home, config.ConfigPath(home))
		os.Exit(1)
	}
	cfg, err = config.Parse(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config parse error: %v\n", err)
		os.Exit(1)
	}
	if err := config.Validate(&cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config invalid: %v\n", err)
		os.Exit(1)
	}
	log = logging.New(cfg.Logging.Level, cfg.Logging.Format)
	return home, cfg, dotenv, log
}

func runServe(o serveOpts) int {
	home, cfg, dotenv, log := loadActive()
	if o.host != "" {
		cfg.Server.Host = o.host
	}
	if o.port != 0 {
		cfg.Server.Port = o.port
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if !isLoopback(cfg.Server.Host) {
		log.Warn("WARNING: sleepyrouter is listening on a non-loopback address.", "host", cfg.Server.Host)
	}
	store, err := config.NewStore(cfg, dotenv, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config invalid: %v\n", err)
		return 1
	}
	ustore := usage.Open(config.UsageDBPath(home), cfg.Usage.Enabled)
	defer ustore.Close()
	aff := state.New(nil)
	reg := provider.DefaultRegistry()
	srv := server.New(server.Deps{Store: store, Usage: ustore, Affinity: aff, Registry: reg, Logger: log, Home: home})

	// Hot reload watcher (parent dir).
	var gen uint64 = 1
	stopWatch, err := config.Watch(home, log, func() {
		dotenv2 := config.LoadDotenv(config.EnvPath(home))
		// Re-read process env each reload (LookupEnv does os.LookupEnv live).
		data, err := os.ReadFile(config.ConfigPath(home))
		if err != nil {
			log.Error("config_reload_failed", "error", err)
			return
		}
		cfg2, err := config.Parse(data)
		if err != nil {
			log.Error("config_reload_failed", "error", err, "active_generation", store.Generation())
			return
		}
		if o.host != "" {
			cfg2.Server.Host = o.host
		}
		if o.port != 0 {
			cfg2.Server.Port = o.port
		}
		gen++
		if err := store.TryReload(cfg2, dotenv2, gen); err != nil {
			log.Error("config_reload_failed", "error", err, "active_generation", store.Generation())
			return
		}
		log.Info("config_reload_success", "generation", store.Generation())
	})
	if err != nil {
		log.Error("watcher failed, continuing with current config", "error", err)
	} else {
		defer stopWatch()
	}

	log.Info("config_loaded", "generation", store.Generation(), "groups", len(cfg.Groups), "models", len(cfg.Models))
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpSrv := &http.Server{Addr: addr, Handler: srv.Handler()}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen %s: %v\n", addr, err)
		return 1
	}
	log.Info("server_started", "addr", addr, "version", buildinfo.Version)
	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Error("serve error", "error", err)
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Info("server_shutdown", "msg", "shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownGrace)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	return 0
}

func isLoopback(host string) bool {
	if host == "127.0.0.1" || host == "localhost" || host == "::1" {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func runValidate() int {
	home := config.ResolveHome()
	data, err := os.ReadFile(config.ConfigPath(home))
	if err != nil {
		fmt.Fprintf(os.Stderr, "config not found: %s\n", config.ConfigPath(home))
		return 1
	}
	cfg, err := config.Parse(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config parse error: %v\n", err)
		return 1
	}
	if err := config.Validate(&cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config invalid: %v\n", err)
		return 1
	}
	fmt.Printf("Config OK\n%d providers\n%d models\n%d groups\n", len(cfg.Providers), len(cfg.Models), len(cfg.Groups))
	return 0
}

func runDoctor() int {
	home := config.ResolveHome()
	fmt.Printf("home: %s\n", home)
	cpath := config.ConfigPath(home)
	if _, err := os.Stat(cpath); err != nil {
		fmt.Printf("config: MISSING (%s)\n", cpath)
		return 1
	}
	fmt.Printf("config: %s\n", cpath)
	data, _ := os.ReadFile(cpath)
	cfg, err := config.Parse(data)
	if err != nil {
		fmt.Printf("config parse: FAIL %v\n", err)
		return 1
	}
	if err := config.Validate(&cfg); err != nil {
		fmt.Printf("config valid: FAIL %v\n", err)
		return 1
	}
	fmt.Printf("config valid: OK\n")
	dotenv := config.LoadDotenv(config.EnvPath(home))
	if _, err := os.Stat(config.EnvPath(home)); err == nil {
		fmt.Printf(".env: present\n")
	} else {
		fmt.Printf(".env: absent\n")
	}
	for _, id := range []string{"zen", "nvidia", "gemini", "openrouter"} {
		envName := ""
		if p, ok := cfg.Providers[id]; ok && p.APIKeyEnv != "" {
			envName = p.APIKeyEnv
		} else {
			_, envName = config.BuiltinProviderDefault(id)
		}
		key, _ := config.LookupEnv(envName, dotenv)
		status := "no"
		if key != "" {
			status = "yes"
		}
		fmt.Printf("provider %-10s key(%s)=%s\n", id, envName, status)
	}
	for _, id := range extraProviders(cfg) {
		fmt.Printf("provider %-10s custom\n", id)
	}
	fmt.Printf("default_group: %s\n", cfg.Routing.DefaultGroup)
	// Port availability.
	addr := fmt.Sprintf("%s:%d", firstNonEmpty(cfg.Server.Host, "127.0.0.1"), cfg.Server.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("port %s: BUSY (%v)\n", addr, err)
	} else {
		fmt.Printf("port %s: available\n", addr)
		ln.Close()
	}
	// usage db writable.
	udb := config.UsageDBPath(home)
	f, err := os.OpenFile(filepath.Join(home, ".writetest"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		fmt.Printf("home writable: FAIL\n")
	} else {
		f.Close()
		os.Remove(filepath.Join(home, ".writetest"))
		fmt.Printf("home writable: OK (%s)\n", udb)
	}
	if p := os.Getenv("HTTP_PROXY"); p != "" || os.Getenv("HTTPS_PROXY") != "" || os.Getenv("https_proxy") != "" {
		fmt.Printf("proxy: detected (upstream uses ProxyFromEnvironment)\n")
	}
	return 0
}

func extraProviders(cfg config.Config) []string {
	var out []string
	for id := range cfg.Providers {
		switch id {
		case "zen", "nvidia", "gemini", "openrouter":
		default:
			out = append(out, id)
		}
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func runModels() int {
	home := config.ResolveHome()
	data, err := os.ReadFile(config.ConfigPath(home))
	if err != nil {
		fmt.Fprintf(os.Stderr, "config not found\n")
		return 1
	}
	cfg, err := config.Parse(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse error: %v\n", err)
		return 1
	}
	dotenv := config.LoadDotenv(config.EnvPath(home))
	for _, g := range cfg.GroupOrd {
		fmt.Printf("GROUP %s\n", g)
		for i, m := range cfg.Groups[g] {
			fmt.Printf("  %d %s\n", i+1, m)
		}
	}
	for id, p := range cfg.Providers {
		key, _ := config.LookupEnv(p.APIKeyEnv, dotenv)
		has := "no"
		if key != "" {
			has = "yes"
		}
		fmt.Printf("%-10s key=%s\n", id, has)
	}
	return 0
}

func runUsage(args []string) int {
	var today, week bool
	var date, model string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--today":
			today = true
		case "--week":
			week = true
		case "--date":
			if i+1 < len(args) {
				date = args[i+1]
				i++
			}
		case "--model":
			if i+1 < len(args) {
				model = args[i+1]
				i++
			}
		default:
			fmt.Fprintf(os.Stderr, "unknown flag %q\n", args[i])
			return 2
		}
	}
	var since, until time.Time
	now := time.Now()
	switch {
	case date != "":
		d, err := time.ParseInLocation("20060102", date, time.Local)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid --date %q (want YYYYMMDD)\n", date)
			return 2
		}
		since, until = d, d.AddDate(0, 0, 1)
	case today:
		y, m, d := now.Date()
		since = time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	case week:
		// ISO week bounds (Monday..Sunday).
		wd := int(now.Weekday())
		if wd == 0 {
			wd = 7
		}
		monday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -(wd - 1))
		since, until = monday, monday.AddDate(0, 0, 7)
	}
	home := config.ResolveHome()
	ustore := usage.Open(config.UsageDBPath(home), true)
	defer ustore.Close()
	sum := ustore.SummaryFiltered(model, since, until)
	fmt.Printf("requests: %d failed: %d input: %d output: %d cache-read: %d cache-write: %d cache-hit: %.1f%%\n",
		sum.Requests, sum.Failed, sum.InputTokens, sum.OutputTokens,
		sum.CachedInputTokens, sum.CacheWriteInputTokens, cacheHitRate(sum.CachedInputTokens, sum.InputTokens))
	for _, m := range sum.ByModel {
		fmt.Printf("  %-30s req=%d fail=%d in=%d out=%d cache-read=%d cache-write=%d cache-hit=%.1f%%\n",
			m.Model, m.Requests, m.Failed, m.InputTokens, m.OutputTokens,
			m.CachedInputTokens, m.CacheWriteInputTokens, cacheHitRate(m.CachedInputTokens, m.InputTokens))
	}
	return 0
}

func cacheHitRate(cachedInputTokens, inputTokens int64) float64 {
	if inputTokens <= 0 {
		return 0
	}
	return 100 * float64(cachedInputTokens) / float64(inputTokens)
}
