import { ConfigStore } from "../config/index.js";

export function runUsageCommand(options: {
  date?: string;
  week?: number;
  store?: ConfigStore;
}): void {
  const store = options.store ?? new ConfigStore();
  let logs = store.readUsageLogs();

  if (options.date) {
    logs = logs.filter((entry) => {
      try {
        const ts = new Date(entry.ts);
        const ymd = `${ts.getFullYear()}${String(ts.getMonth() + 1).padStart(2, "0")}${String(ts.getDate()).padStart(2, "0")}`;
        return ymd === options.date;
      } catch {
        return false;
      }
    });
  } else if (options.week) {
    logs = logs.filter((entry) => {
      try {
        const ts = new Date(entry.ts);
        const startOfYear = new Date(ts.getFullYear(), 0, 1);
        const days = Math.floor(
          (ts.getTime() - startOfYear.getTime()) / 86400000,
        );
        const weekNum = Math.ceil((days + startOfYear.getDay() + 1) / 7);
        return weekNum === options.week;
      } catch {
        return false;
      }
    });
  }

  if (logs.length === 0) {
    let filterDesc = "";
    if (options.date) filterDesc = ` (날짜: ${options.date})`;
    else if (options.week) filterDesc = ` (주차: ${options.week}주차)`;
    console.log(`사용 기록이 없어요${filterDesc}.`);
    return;
  }

  const byModel = new Map<
    string,
    {
      model: string;
      requests: number;
      failed: number;
      inputTokens: number;
      outputTokens: number;
    }
  >();
  for (const entry of logs) {
    let row = byModel.get(entry.model);
    if (!row) {
      row = {
        model: entry.model,
        requests: 0,
        failed: 0,
        inputTokens: 0,
        outputTokens: 0,
      };
      byModel.set(entry.model, row);
    }
    row.requests++;
    if (!entry.success) row.failed++;
    row.inputTokens += entry.inputTokens;
    row.outputTokens += entry.outputTokens;
  }

  const rows = [...byModel.values()].sort((a, b) => {
    if (a.requests !== b.requests) return b.requests - a.requests;
    if (a.inputTokens !== b.inputTokens) return b.inputTokens - a.inputTokens;
    return a.model.localeCompare(b.model);
  });

  console.log("\n모델별 사용량:");
  console.log(
    "모델".padEnd(40) +
      "요청".padStart(8) +
      "실패".padStart(8) +
      "입력토큰".padStart(12) +
      "출력토큰".padStart(12),
  );
  console.log("-".repeat(80));

  let totalRequests = 0;
  let totalFailed = 0;
  let totalInput = 0;
  let totalOutput = 0;

  for (const row of rows) {
    totalRequests += row.requests;
    totalFailed += row.failed;
    totalInput += row.inputTokens;
    totalOutput += row.outputTokens;
    console.log(
      row.model.padEnd(40) +
        String(row.requests).padStart(8) +
        String(row.failed).padStart(8) +
        String(row.inputTokens).padStart(12) +
        String(row.outputTokens).padStart(12),
    );
  }
  console.log("-".repeat(80));
  console.log(
    "합계".padEnd(40) +
      String(totalRequests).padStart(8) +
      String(totalFailed).padStart(8) +
      String(totalInput).padStart(12) +
      String(totalOutput).padStart(12),
  );
}
