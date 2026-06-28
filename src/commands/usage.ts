import { ConfigStore } from '../config/store.js';
import { UsageLogEntry } from '../types.js';
import Table from 'cli-table3';

export interface RunUsageCommandOptions {
  date?: string;  // YYYYMMDD
  week?: number;  // ISO week number
  store?: ConfigStore;
}

function getWeekNumber(d: Date): number {
  const start = new Date(d.getFullYear(), 0, 1);
  const days = Math.floor((d.getTime() - start.getTime()) / 86400000);
  return Math.ceil((days + start.getDay() + 1) / 7);
}

function filterLogs(logs: UsageLogEntry[], date?: string, week?: number): UsageLogEntry[] {
  if (!date && !week) return logs;
  return logs.filter((entry) => {
    const d = new Date(entry.ts);
    if (date) {
      const ymd = `${d.getFullYear()}${String(d.getMonth() + 1).padStart(2, '0')}${String(d.getDate()).padStart(2, '0')}`;
      if (ymd !== date) return false;
    }
    if (week) {
      const y = d.getFullYear();
      const w = getWeekNumber(d);
      const targetYear = new Date().getFullYear();
      if (y !== targetYear || w !== week) return false;
    }
    return true;
  });
}

function aggregate(logs: UsageLogEntry[]): Array<{ model: string; requests: number; failed: number; inputTokens: number; outputTokens: number }> {
  const map = new Map<string, { requests: number; failed: number; inputTokens: number; outputTokens: number }>();
  for (const entry of logs) {
    const row = map.get(entry.model) ?? { requests: 0, failed: 0, inputTokens: 0, outputTokens: 0 };
    row.requests += 1;
    if (!entry.success) row.failed += 1;
    row.inputTokens += entry.inputTokens;
    row.outputTokens += entry.outputTokens;
    map.set(entry.model, row);
  }
  return [...map.entries()]
    .map(([model, data]) => ({ model, ...data }))
    .sort((a, b) => b.requests - a.requests || b.inputTokens - a.inputTokens || a.model.localeCompare(b.model));
}

export function runUsageCommand(options: RunUsageCommandOptions = {}): void {
  const store = options.store ?? new ConfigStore();
  const logs = filterLogs(store.readUsageLogs(), options.date, options.week);

  if (logs.length === 0) {
    console.log('아직 사용 기록이 없어요.');
    return;
  }

  const rows = aggregate(logs);

  const table = new Table({
    head: ['Model ID', 'Requests', 'Failed', 'Input Tokens', 'Output Tokens'],
    colWidths: [56, 10, 8, 14, 14],
    style: { head: ['cyan'] },
  });

  for (const row of rows) {
    table.push([
      row.model,
      String(row.requests),
      String(row.failed),
      String(row.inputTokens),
      String(row.outputTokens),
    ]);
  }

  console.log(table.toString());
}
