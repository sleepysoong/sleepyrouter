package cli

import (
	"os"
	"testing"
	"time"

	"github.com/sleepysoong/sleepyrouter/internal/cfg"
	"github.com/sleepysoong/sleepyrouter/internal/types"
)

func TestRunUsageCommand_Empty(t *testing.T) {
	root, err := os.MkdirTemp("", "sleepyrouter-usage-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	store := cfg.NewConfigStore(root)
	RunUsageCommand(UsageCommandOptions{Store: store})
}

func TestRunUsageCommand_Aggregated(t *testing.T) {
	root, err := os.MkdirTemp("", "sleepyrouter-usage-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	store := cfg.NewConfigStore(root)
	_ = store.AppendUsage(types.UsageLogEntry{TS: "2026-06-28T10:00:00Z", Model: "beta", InputTokens: 0, OutputTokens: 0, Success: true})
	_ = store.AppendUsage(types.UsageLogEntry{TS: "2026-06-28T10:01:00Z", Model: "alpha", InputTokens: 1, OutputTokens: 2, Success: true})
	_ = store.AppendUsage(types.UsageLogEntry{TS: "2026-06-28T10:02:00Z", Model: "alpha", InputTokens: 0, OutputTokens: 0, Success: false})

	logs, err := store.ReadUsageLogs()
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 logs, got %d", len(logs))
	}

	// Check aggregation
	rows := aggregateUsage(logs)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	// alpha should come first (2 requests vs 1)
	if rows[0].Model != "alpha" || rows[0].Requests != 2 || rows[0].Failed != 1 {
		t.Fatalf("row[0]: %+v", rows[0])
	}
	if rows[0].InputTokens != 1 {
		t.Fatalf("alpha inputTokens: %d", rows[0].InputTokens)
	}
	if rows[1].Model != "beta" {
		t.Fatalf("row[1]: %+v", rows[1])
	}

	RunUsageCommand(UsageCommandOptions{Store: store})
}

func TestFilterUsageLogs_ByDate(t *testing.T) {
	logs := []types.UsageLogEntry{
		{TS: "2026-06-28T10:00:00Z", Model: "a", InputTokens: 1, OutputTokens: 0, Success: true},
		{TS: "2026-06-29T10:00:00Z", Model: "b", InputTokens: 1, OutputTokens: 0, Success: true},
		{TS: "2026-06-28T11:00:00Z", Model: "a", InputTokens: 1, OutputTokens: 0, Success: true},
	}
	filtered := filterUsageLogs(logs, "20260628", 0)
	if len(filtered) != 2 {
		t.Fatalf("expected 2, got %d", len(filtered))
	}
	if filtered[0].Model != "a" || filtered[1].Model != "a" {
		t.Fatalf("models: %+v", filtered)
	}
}

func TestFilterUsageLogs_ByWeek(t *testing.T) {
	ts := "2026-06-28T10:00:00Z"
	tm, _ := time.Parse(time.RFC3339, ts)
	_, wn := tm.ISOWeek()

	logs := []types.UsageLogEntry{
		{TS: ts, Model: "a", InputTokens: 1, OutputTokens: 0, Success: true},
		{TS: "2026-07-01T10:00:00Z", Model: "b", InputTokens: 1, OutputTokens: 0, Success: true},
	}
	filtered := filterUsageLogs(logs, "", wn)
	if len(filtered) == 0 {
		t.Fatalf("expected at least 1, got 0 (week %d)", wn)
	}
}
