package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/types"
)

type exchangeRateResponse struct {
	Result string             `json:"result"`
	Rates  map[string]float64 `json:"rates"`
}

func fetchKRWRate() (float64, error) {
	resp, err := http.Get("https://open.er-api.com/v6/latest/USD")
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	var data exchangeRateResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, err
	}
	if data.Result != "success" {
		return 0, fmt.Errorf("API result: %s", data.Result)
	}
	rate, ok := data.Rates["KRW"]
	if !ok || rate == 0 {
		return 0, fmt.Errorf("KRW rate not found")
	}
	return rate, nil
}

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
	Cost         float64
}

func aggregateUsage(logs []types.UsageLogEntry, prices map[string]types.ModelDefinition) []usageAggregateRow {
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
		r := *m[model]
		if def, ok := prices[model]; ok && (def.InputPrice > 0 || def.OutputPrice > 0) {
			inCost := float64(r.InputTokens) * def.InputPrice / 1_000_000
			outCost := float64(r.OutputTokens) * def.OutputPrice / 1_000_000
			r.Cost = inCost + outCost
		} else {
			r.Cost = -1 // ponytail: sentinel for N/A — excluded from totals
		}
		result = append(result, r)
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

	config, _ := store.ReadConfig()
	rows := aggregateUsage(logs, config.Models)

	krwRate, _ := fetchKRWRate()

	totalRequests := 0
	totalFailed := 0
	totalInput := 0
	totalOutput := 0
	totalCost := 0.0
	naCount := 0
	for _, row := range rows {
		totalRequests += row.Requests
		totalFailed += row.Failed
		totalInput += row.InputTokens
		totalOutput += row.OutputTokens
		if row.Cost >= 0 {
			totalCost += row.Cost
		} else {
			naCount++
		}
	}

	// ponytail: tablewriter v1 auto-sizes columns to content — no hardcoded widths
	cfg := tablewriter.Config{
		Header: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignLeft},
		},
		Row: tw.CellConfig{
			Alignment: tw.CellAlignment{
				Global: tw.AlignLeft,
				PerColumn: []tw.Align{
					tw.AlignLeft,
					tw.AlignRight,
					tw.AlignRight,
					tw.AlignRight,
					tw.AlignRight,
					tw.AlignRight,
				},
			},
		},
		Footer: tw.CellConfig{
			Alignment: tw.CellAlignment{Global: tw.AlignRight},
		},
	}
	table := tablewriter.NewTable(os.Stdout, tablewriter.WithConfig(cfg))
	table.Header([]string{"Model", "Requests", "Failed", "Input", "Output", "Cost"})
	for _, row := range rows {
		cost := "N/A"
		if row.Cost >= 0 {
			cost = fmt.Sprintf("$%.4f", row.Cost)
		}
		_ = table.Append([]string{
			row.Model,
			strconv.Itoa(row.Requests),
			strconv.Itoa(row.Failed),
			strconv.Itoa(row.InputTokens),
			strconv.Itoa(row.OutputTokens),
			cost,
		})
	}

	// Summary footer
	summary := fmt.Sprintf("총 %d건 요청, %d건 실패, in=%d out=%d cost=$%.4f", totalRequests, totalFailed, totalInput, totalOutput, totalCost)
	if krwRate > 0 {
		summary += fmt.Sprintf(" (₩%d)", int(totalCost*krwRate))
	}
	if naCount > 0 {
		summary += fmt.Sprintf(" — %d개 모델 가격 미설정", naCount)
	}
	table.Footer([]string{"", "", "", "", "", summary})

	filterDesc := "전체"
	if options.Date != "" {
		filterDesc = fmt.Sprintf("날짜: %s", options.Date)
	} else if options.Week != 0 {
		filterDesc = fmt.Sprintf("주차: %d주차", options.Week)
	}
	fmt.Printf("사용량 (%s)\n", filterDesc)
	_ = table.Render()
}
