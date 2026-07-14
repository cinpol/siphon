package views

import (
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
