package usage

import (
	"database/sql"
	"testing"
)

func TestMigrateAddsCacheColumnsAndPreservesExistingRows(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE requests (
		request_id TEXT PRIMARY KEY,
		started_at TEXT NOT NULL,
		completed_at TEXT,
		protocol TEXT NOT NULL,
		requested_model TEXT NOT NULL,
		routed_model TEXT,
		provider TEXT,
		attempts INTEGER NOT NULL,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		success INTEGER NOT NULL,
		error_class TEXT,
		duration_ms INTEGER NOT NULL,
		claude_session_id TEXT,
		config_generation INTEGER NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO requests
		(request_id, started_at, protocol, requested_model, attempts, input_tokens, output_tokens, success, duration_ms, config_generation)
		VALUES ('old', datetime('now'), 'openai', 'model-a', 1, 9, 2, 1, 10, 1)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate legacy DB: %v", err)
	}
	var id string
	var input, cached, written int64
	if err := db.QueryRow(`SELECT request_id,input_tokens,cached_input_tokens,cache_write_input_tokens FROM requests WHERE request_id='old'`).Scan(&id, &input, &cached, &written); err != nil {
		t.Fatal(err)
	}
	if id != "old" || input != 9 || cached != 0 || written != 0 {
		t.Fatalf("legacy row after migration: id=%q input=%d cached=%d written=%d", id, input, cached, written)
	}
	_, err = db.Exec(`INSERT INTO requests
		(request_id, started_at, protocol, requested_model, attempts, input_tokens, cached_input_tokens, cache_write_input_tokens, output_tokens, success, duration_ms, config_generation)
		VALUES ('new', datetime('now'), 'openai', 'model-a', 1, 100, 80, 20, 5, 1, 10, 1)`)
	if err != nil {
		t.Fatalf("insert cache usage: %v", err)
	}
	if err := db.QueryRow(`SELECT input_tokens,cached_input_tokens,cache_write_input_tokens FROM requests WHERE request_id='new'`).Scan(&input, &cached, &written); err != nil {
		t.Fatal(err)
	}
	if input != 100 || cached != 80 || written != 20 {
		t.Fatalf("new usage row: input=%d cached=%d written=%d", input, cached, written)
	}
}
