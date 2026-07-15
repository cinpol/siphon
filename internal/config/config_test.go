package config

import (
	"testing"

	"github.com/cinpol/siphon/internal/model"
)

func TestProblemFlags(t *testing.T) {
	// Unset -> the built-in default.
	if got := (UIConfig{}).ProblemFlags(); len(got) != len(model.DefaultPGProblemFlags) {
		t.Errorf("unset ProblemFlags() = %v, want default %v", got, model.DefaultPGProblemFlags)
	}
	// Set -> replaces the default entirely.
	custom := []string{"inconsistent", "backfill_toofull"}
	got := UIConfig{PGProblemFlags: custom}.ProblemFlags()
	if len(got) != 2 || got[0] != "inconsistent" || got[1] != "backfill_toofull" {
		t.Errorf("ProblemFlags() = %v, want %v", got, custom)
	}
}

func TestPoolRows(t *testing.T) {
	cases := []struct {
		name string
		set  int
		want int
	}{
		{"unset defaults", 0, defaultDashboardPoolRows},
		{"negative defaults", -3, defaultDashboardPoolRows},
		{"explicit value honoured", 12, 12},
		{"one is allowed", 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u := UIConfig{DashboardPoolRows: c.set}
			if got := u.PoolRows(); got != c.want {
				t.Errorf("PoolRows() = %d, want %d", got, c.want)
			}
		})
	}
}

// TestDefaultPoolRows guards that the built-in default config carries the
// documented default row count.
func TestDefaultPoolRows(t *testing.T) {
	if got := Default().UI.PoolRows(); got != defaultDashboardPoolRows {
		t.Errorf("default PoolRows() = %d, want %d", got, defaultDashboardPoolRows)
	}
}
