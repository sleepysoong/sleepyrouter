"""Usage logging to SQLite database."""

from pathlib import Path
import sqlite3

from sleepyrouter.types import UsageLogEntry


class UsageLogger:
    def __init__(self, root: Path):
        self.db_path = root / "usage.db"
        self._conn: sqlite3.Connection | None = None

    def _init_db(self) -> sqlite3.Connection:
        if self._conn is None:
            self._conn = sqlite3.connect(str(self.db_path), check_same_thread=False)
            with self._conn:
                self._conn.execute(
                    """CREATE TABLE IF NOT EXISTS usage_log (
                        ts TEXT NOT NULL,
                        model TEXT NOT NULL,
                        input_tokens INTEGER NOT NULL,
                        output_tokens INTEGER NOT NULL,
                        success INTEGER NOT NULL
                    )"""
                )
        return self._conn

    def append_usage(self, entry: UsageLogEntry) -> None:
        try:
            conn = self._init_db()
            with conn:
                conn.execute(
                    """INSERT INTO usage_log
                       (ts, model, input_tokens, output_tokens, success)
                       VALUES (?, ?, ?, ?, ?)""",
                    (
                        entry.ts,
                        entry.model,
                        entry.input_tokens,
                        entry.output_tokens,
                        1 if entry.success else 0,
                    ),
                )
        except sqlite3.Error:
            pass

    def read_usage_logs(self) -> list[UsageLogEntry]:
        try:
            conn = self._init_db()
            cursor = conn.execute(
                "SELECT ts, model, input_tokens, output_tokens, success FROM usage_log ORDER BY ts"
            )
            rows = cursor.fetchall()
            return [
                UsageLogEntry(
                    ts=r[0],
                    model=r[1],
                    input_tokens=r[2],
                    output_tokens=r[3],
                    success=bool(r[4]),
                )
                for r in rows
            ]
        except sqlite3.Error:
            return []

    def close(self) -> None:
        if self._conn:
            self._conn.close()
            self._conn = None
