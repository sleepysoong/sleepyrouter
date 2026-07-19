package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/types"
)

type UsageCommandOptions struct {
	Date  string
	Week  int
	Store *cfg.ConfigStore
}

func filterUsageLogs(logs []types.UsageLogEntry, date string, week int) []types.UsageLogEntry {
	if date == "" && week == 0 {
		return logs
	}
	result := make([]types.UsageLogEntry, 0, len(logs))
	for _, entry := range logs {
		ts, err := time.Parse(time.RFC3339, entry.TS)
		if err != nil {
			continue
		}
		if date != "" {
			ymd := fmt.Sprintf("%04d%02d%02d", ts.Year(), int(ts.Month()), ts.Day())
			if ymd != date {
				continue
			}
		}
		if week != 0 {
			isoYear, isoWeek := ts.ISOWeek()
			targetISOYear, _ := time.Now().ISOWeek()
			if isoYear != targetISOYear || isoWeek != week {
				continue
			}
		}
		result = append(result, entry)
	}
	return result
}

type usageAggregateRow struct {
	Model        string
	Requests     int
	Failed       int
	InputTokens  int
	OutputTokens int
}

func aggregateUsage(logs []types.UsageLogEntry) []usageAggregateRow {
	m := make(map[string]*usageAggregateRow)
	order := make([]string, 0)
	for _, entry := range logs {
		row, exists := m[entry.Model]
		if !exists {
			row = &usageAggregateRow{Model: entry.Model}
			m[entry.Model] = row
			order = append(order, entry.Model)
		}
		row.Requests++
		if !entry.Success {
			row.Failed++
		}
		row.InputTokens += entry.InputTokens
		row.OutputTokens += entry.OutputTokens
	}
	result := make([]usageAggregateRow, 0, len(order))
	for _, model := range order {
		result = append(result, *m[model])
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Requests != result[j].Requests {
			return result[i].Requests > result[j].Requests
		}
		if result[i].InputTokens != result[j].InputTokens {
			return result[i].InputTokens > result[j].InputTokens
		}
		return result[i].Model < result[j].Model
	})
	return result
}

func RunUsageCommand(options UsageCommandOptions) {
	store := options.Store
	if store == nil {
		store = cfg.NewConfigStore("")
	}
	logs, _ := store.ReadUsageLogs()
	logs = filterUsageLogs(logs, options.Date, options.Week)

	if len(logs) == 0 {
		filterDesc := ""
		if options.Date != "" {
			filterDesc = fmt.Sprintf(" (날짜: %s)", options.Date)
		} else if options.Week != 0 {
			filterDesc = fmt.Sprintf(" (주차: %d주차)", options.Week)
		}
		fmt.Printf("사용 기록이 없어요%s.\n", filterDesc)
		return
	}

	rows := aggregateUsage(logs)

	totalRequests := 0
	totalFailed := 0
	totalInput := 0
	totalOutput := 0
	for _, row := range rows {
		totalRequests += row.Requests
		totalFailed += row.Failed
		totalInput += row.InputTokens
		totalOutput += row.OutputTokens
	}

	// Build a simple table similar to cli-table3
	var sb strings.Builder
	sb.WriteString("┌──────────────────────────────────────────────────────┬──────────┬────────┬──────────────┬──────────────┐\n")
	sb.WriteString("│ Model ID                                             │ Requests │ Failed │ Input Tokens │ Output Token │\n")
	sb.WriteString("├──────────────────────────────────────────────────────┼──────────┼────────┼──────────────┼──────────────┤\n")
	for _, row := range rows {
		id := padRight(row.Model, 52)
		req := padLeft(strconv.Itoa(row.Requests), 8)
		failed := padLeft(strconv.Itoa(row.Failed), 6)
		in := padLeft(strconv.Itoa(row.InputTokens), 12)
		out := padLeft(strconv.Itoa(row.OutputTokens), 12)
		sb.WriteString(fmt.Sprintf("│ %s │ %s │ %s │ %s │ %s │\n", id, req, failed, in, out))
	}
	// Summary row
	summary := fmt.Sprintf("총 %d건 요청, %d건 실패, in=%d out=%d", totalRequests, totalFailed, totalInput, totalOutput)
	blank := padRight("", 52)
	sb.WriteString("├──────────────────────────────────────────────────────┴──────────┴────────┴──────────────┴──────────────┤\n")
	sb.WriteString(fmt.Sprintf("│ %s %s │\n", blank, padRight(summary, 46)))
	sb.WriteString("└──────────────────────────────────────────────────────────────────────────────────────────────────────────┘\n")

	filterDesc := "전체"
	if options.Date != "" {
		filterDesc = fmt.Sprintf("날짜: %s", options.Date)
	} else if options.Week != 0 {
		filterDesc = fmt.Sprintf("주차: %d주차", options.Week)
	}
	fmt.Printf("사용량 (%s)\n", filterDesc)
	fmt.Print(sb.String())
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

func padLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}
