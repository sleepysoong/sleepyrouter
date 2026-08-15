import { Database, type Statement } from "bun:sqlite";
import { join } from "node:path";
import type { UsageLogEntry } from "../types.js";

export class UsageLogger {
  private db: Database | null = null;
  private dbPath: string;
  private insertStmt: Statement | null = null;
  private selectStmt: Statement | null = null;

  constructor(root: string) {
    this.dbPath = join(root, "usage.db");
  }

  private initDB(): Database {
    if (!this.db) {
      this.db = new Database(this.dbPath);
      this.db.run(`CREATE TABLE IF NOT EXISTS usage_log (
        ts TEXT NOT NULL,
        model TEXT NOT NULL,
        input_tokens INTEGER NOT NULL,
        output_tokens INTEGER NOT NULL,
        success INTEGER NOT NULL
      )`);
      this.insertStmt = this.db.prepare(
        `INSERT INTO usage_log (ts, model, input_tokens, output_tokens, success) VALUES (?, ?, ?, ?, ?)`,
      );
      this.selectStmt = this.db.prepare(
        `SELECT ts, model, input_tokens as inputTokens, output_tokens as outputTokens, success FROM usage_log ORDER BY ts`,
      );
    }
    return this.db;
  }

  appendUsage(entry: UsageLogEntry): void {
    try {
      this.initDB();
      this.insertStmt?.run(
        entry.ts,
        entry.model,
        entry.inputTokens,
        entry.outputTokens,
        entry.success ? 1 : 0,
      );
    } catch {
      // Best-effort usage logging
    }
  }

  readUsageLogs(): UsageLogEntry[] {
    try {
      this.initDB();
      const rows = (this.selectStmt?.all() ?? []) as Array<{
        ts: string;
        model: string;
        inputTokens: number;
        outputTokens: number;
        success: number;
      }>;
      return rows.map((r) => ({
        ts: r.ts,
        model: r.model,
        inputTokens: r.inputTokens,
        outputTokens: r.outputTokens,
        success: r.success === 1,
      }));
    } catch {
      return [];
    }
  }

  close(): void {
    this.insertStmt = null;
    this.selectStmt = null;
    this.db?.close();
    this.db = null;
  }
}
