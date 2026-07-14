package views

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cinpol/siphon/internal/model"
)

// healthWith builds a Health with one check carrying n detail lines.
func healthWith(n int) model.Health {
	details := make([]string, n)
	for i := range details {
		details[i] = "detail line"
	}
	return model.Health{
		Status: model.HealthWarn,
		Checks: []model.HealthCheck{{
			Code:     "PG_NOT_DEEP_SCRUBBED",
			Severity: "HEALTH_WARN",
			Summary:  "pgs not deep-scrubbed in time",
			Details:  details,
		}},
	}
}

// TestHealthLinesLimitCountsWithoutStyling checks the core of the perf fix: with
// a limit, only limit lines are materialised, but the returned total still counts
// every line (so the "+N more" hint is correct without styling the whole list).
func TestHealthLinesLimitCounts(t *testing.T) {
	h := healthWith(40) // 1 code + 1 summary + 40 details = 42 lines total
	lines, total := healthLines(h, healthPreviewLines)
	if total != 42 {
		t.Errorf("total = %d, want 42", total)
	}
	if len(lines) != healthPreviewLines {
		t.Errorf("styled lines = %d, want %d (should stop at the limit)", len(lines), healthPreviewLines)
	}

	// Limit 0 means "all", used by the scrollable overlay.
	all, total := healthLines(h, 0)
	if len(all) != 42 || total != 42 {
		t.Errorf("unlimited: got %d lines / total %d, want 42/42", len(all), total)
	}
}

// TestHealthDetailPanelTruncates verifies the inline panel truncates long output
// with an accurate hint, and shows short output in full (no regression).
func TestHealthDetailPanelTruncates(t *testing.T) {
	long := healthDetailPanel(healthWith(40), 80)
	if !strings.Contains(long, "+35 more — press enter to view") { // 42 - (8-1) = 35
		t.Errorf("expected a '+35 more' hint:\n%s", long)
	}

	short := healthDetailPanel(healthWith(2), 80) // 4 lines total, under budget
	if strings.Contains(short, "more — press enter to view") {
		t.Errorf("short output should not be truncated:\n%s", short)
	}
}

// poolsWith builds n pools with ascending fullness (pool-00 emptiest).
func poolsWith(n int) []model.Pool {
	pools := make([]model.Pool, n)
	for i := range pools {
		pools[i] = model.Pool{
			Name:        fmt.Sprintf("pool-%02d", i),
			UsedRatio:   float64(i) / float64(n),
			StoredBytes: int64(i) * 1_000_000_000,
		}
	}
	return pools
}

// TestPoolCapacityPanelSortsAndTruncates verifies the dashboard shows the fullest
// pools first and truncates a long list with an accurate pointer to the Pools
// view.
func TestPoolCapacityPanelSortsAndTruncates(t *testing.T) {
	out := poolCapacityPanel(poolsWith(40), 90, 5)

	// Fullest pool (pool-39) is shown; the emptiest (pool-00) is not.
	if !strings.Contains(out, "pool-39") {
		t.Errorf("expected the fullest pool to be listed:\n%s", out)
	}
	if strings.Contains(out, "pool-00") {
		t.Errorf("emptiest pool should be truncated away:\n%s", out)
	}
	// 40 pools, 5 shown -> 35 hidden, pointing at the Pools view.
	if !strings.Contains(out, "+35 more pools — press 3 for Pools") {
		t.Errorf("expected a '+35 more pools' pointer:\n%s", out)
	}
}

// TestPoolCapacityPanelShort verifies no truncation with a handful of pools, and
// an empty result for a cluster with no pools.
func TestPoolCapacityPanelShort(t *testing.T) {
	out := poolCapacityPanel(poolsWith(3), 90, 5)
	if strings.Contains(out, "more pool") {
		t.Errorf("3 pools should not be truncated:\n%s", out)
	}
	for _, name := range []string{"pool-00", "pool-01", "pool-02"} {
		if !strings.Contains(out, name) {
			t.Errorf("expected %s to be shown:\n%s", name, out)
		}
	}
	if got := poolCapacityPanel(nil, 90, 5); got != "" {
		t.Errorf("no pools should render nothing, got %q", got)
	}
}

// TestPoolCapacityPanelConfigurableRows verifies the row count is honoured: a
// smaller configured value shows fewer pools and truncates the rest.
func TestPoolCapacityPanelConfigurableRows(t *testing.T) {
	out := poolCapacityPanel(poolsWith(40), 90, 3)
	for _, name := range []string{"pool-39", "pool-38", "pool-37"} {
		if !strings.Contains(out, name) {
			t.Errorf("expected top-3 pool %s with rows=3:\n%s", name, out)
		}
	}
	if strings.Contains(out, "pool-36") {
		t.Errorf("rows=3 should not show a 4th pool:\n%s", out)
	}
	if !strings.Contains(out, "+37 more pools — press 3 for Pools") {
		t.Errorf("rows=3 of 40 -> 37 hidden:\n%s", out)
	}
}
