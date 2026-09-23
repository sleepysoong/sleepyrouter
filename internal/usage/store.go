package usage

import (
	"database/sql"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store writes usage records; failures never fail inference.
type Store struct {
	enabled bool
	db      *sql.DB
	mu      sync.Mutex
	ch      chan func()
}

func Open(path string, enabled bool) *Store {
	s := &Store{enabled: enabled, ch: make(chan func(), 1024)}
	if !enabled {
		go s.loop(nil)
		return s
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		go s.loop(nil)
		return s
	}
	_, _ = db.Exec(`PRAGMA journal_mode=WAL`)
	_, _ = db.Exec(`PRAGMA busy_timeout=5000`)
	_, _ = db.Exec(`PRAGMA synchronous=NORMAL`)
	_ = Migrate(db)
	s.db = db
	go s.loop(db)
	return s
}

func (s *Store) loop(db *sql.DB) {
	for fn := range s.ch {
		func() {
			defer func() { _ = recover() }()
			fn()
		}()
		_ = db
	}
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// RecordRequest enqueues async insert (non-blocking, drops on full).
func (s *Store) RecordRequest(r Record, attempts []Attempt) {
	if !s.enabled || s.db == nil {
		return
	}
	select {
	case s.ch <- func() { s.insert(r, attempts) }:
	default:
	}
}

func (s *Store) insert(r Record, attempts []Attempt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	success := 0
	if r.Success {
		success = 1
	}
	_, _ = s.db.Exec(`INSERT OR REPLACE INTO requests
		(request_id, started_at, completed_at, protocol, requested_model, routed_model, provider, attempts, input_tokens, cached_input_tokens, cache_write_input_tokens, output_tokens, success, error_class, duration_ms, claude_session_id, config_generation)
		VALUES (?, datetime('now'), datetime('now'), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.RequestID, r.Protocol, r.RequestedModel, r.RoutedModel, r.Provider, r.Attempts,
		r.InputTokens, r.CachedInputTokens, r.CacheWriteInputTokens, r.OutputTokens, success, r.ErrorClass, r.DurationMs, r.SessionID, r.ConfigGen)
	for _, a := range attempts {
		as := 0
		if a.Success {
			as = 1
		}
		_, _ = s.db.Exec(`INSERT OR REPLACE INTO attempts
			(request_id, attempt_index, model, provider, started_at, duration_ms, success, status_code, error_class)
			VALUES (?, ?, ?, ?, datetime('now'), ?, ?, ?, ?)`,
			a.RequestID, a.Index, a.Model, a.Provider, a.DurationMs, as, a.StatusCode, a.ErrorClass)
	}
}

// Summary aggregates for CLI.
type Summary struct {
	Requests              int64
	Failed                int64
	InputTokens           int64
	CachedInputTokens     int64
	CacheWriteInputTokens int64
	OutputTokens          int64
	ByModel               []ModelRow
}

// ModelRow is per-model aggregate.
type ModelRow struct {
	Model                 string
	Requests              int64
	Failed                int64
	InputTokens           int64
	CachedInputTokens     int64
	CacheWriteInputTokens int64
	OutputTokens          int64
}

func (s *Store) Summary() Summary { return s.SummaryFiltered("", time.Time{}, time.Time{}) }

// SummaryFiltered aggregates requests constrained by model (exact match on
// routed or requested model, empty = all) and started_at range
// (zero time = unbounded).
func (s *Store) SummaryFiltered(model string, since, until time.Time) Summary {
	var out Summary
	if !s.enabled || s.db == nil {
		return out
	}
	where, args := filteredWhere(model, since, until)
	_ = s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN success=0 THEN 1 ELSE 0 END),0), COALESCE(SUM(input_tokens),0), COALESCE(SUM(cached_input_tokens),0), COALESCE(SUM(cache_write_input_tokens),0), COALESCE(SUM(output_tokens),0) FROM requests`+where, args...).Scan(&out.Requests, &out.Failed, &out.InputTokens, &out.CachedInputTokens, &out.CacheWriteInputTokens, &out.OutputTokens)
	rows, err := s.db.Query(`SELECT COALESCE(NULLIF(routed_model,''), requested_model), COUNT(*), COALESCE(SUM(CASE WHEN success=0 THEN 1 ELSE 0 END),0), COALESCE(SUM(input_tokens),0), COALESCE(SUM(cached_input_tokens),0), COALESCE(SUM(cache_write_input_tokens),0), COALESCE(SUM(output_tokens),0) FROM requests`+where+` GROUP BY COALESCE(NULLIF(routed_model,''), requested_model) ORDER BY COUNT(*) DESC`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var m ModelRow
		if err := rows.Scan(&m.Model, &m.Requests, &m.Failed, &m.InputTokens, &m.CachedInputTokens, &m.CacheWriteInputTokens, &m.OutputTokens); err == nil {
			out.ByModel = append(out.ByModel, m)
		}
	}
	return out
}

func filteredWhere(model string, since, until time.Time) (string, []any) {
	var conds []string
	var args []any
	if model != "" {
		conds = append(conds, `(routed_model = ? OR requested_model = ?)`)
		args = append(args, model, model)
	}
	if !since.IsZero() {
		conds = append(conds, `started_at >= ?`)
		args = append(args, since.UTC().Format("2006-01-02 15:04:05"))
	}
	if !until.IsZero() {
		conds = append(conds, `started_at < ?`)
		args = append(args, until.UTC().Format("2006-01-02 15:04:05"))
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}
