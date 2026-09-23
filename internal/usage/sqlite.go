package usage

import "database/sql"

// Migrate creates requests/attempts tables.
func Migrate(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS requests (
		request_id TEXT PRIMARY KEY,
		started_at TEXT NOT NULL,
		completed_at TEXT,
		protocol TEXT NOT NULL,
		requested_model TEXT NOT NULL,
		routed_model TEXT,
		provider TEXT,
		attempts INTEGER NOT NULL,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		cached_input_tokens INTEGER NOT NULL DEFAULT 0,
		cache_write_input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		success INTEGER NOT NULL,
		error_class TEXT,
		duration_ms INTEGER NOT NULL,
		claude_session_id TEXT,
		config_generation INTEGER NOT NULL
	)`)
	if err != nil {
		return err
	}
	// Existing personal usage databases predate the cache-token columns.
	// Add them in place so usage history survives upgrades.
	for _, column := range []string{"cached_input_tokens", "cache_write_input_tokens"} {
		if err := ensureColumn(db, "requests", column); err != nil {
			return err
		}
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS attempts (
		request_id TEXT NOT NULL,
		attempt_index INTEGER NOT NULL,
		model TEXT NOT NULL,
		provider TEXT NOT NULL,
		started_at TEXT NOT NULL,
		duration_ms INTEGER NOT NULL,
		success INTEGER NOT NULL,
		status_code INTEGER,
		error_class TEXT,
		PRIMARY KEY (request_id, attempt_index)
	)`)
	return err
}

func ensureColumn(db *sql.DB, table, column string) error {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` INTEGER NOT NULL DEFAULT 0`)
	return err
}
