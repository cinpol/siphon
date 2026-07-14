package config

import "testing"

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
