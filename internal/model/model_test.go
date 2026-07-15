package model

import "testing"

// TestPGHealthy guards the "problems only" classification: a PG is healthy only
// when active+clean with no error flag appended. The tricky cases are states
// that *contain* "active+clean" but carry an error flag (inconsistent, etc.) —
// they must count as problems, while benign scrub/snaptrim flags stay healthy.
func TestPGHealthy(t *testing.T) {
	cases := []struct {
		state string
		want  bool
	}{
		{"active+clean", true},
		{"active+clean+scrubbing", true},
		{"active+clean+scrubbing+deep", true},
		{"active+clean+snaptrim", true},
		{"active+clean+snaptrim_wait", true},

		{"active+clean+inconsistent", false}, // the reported bug
		{"active+clean+scrubbing+deep+inconsistent", false},
		{"active+clean+snaptrim_error", false},
		{"active+clean+inconsistent+failed_repair", false},
		{"active+recovery_unfound+degraded", false},

		{"active+remapped+backfilling", false},
		{"active+undersized+degraded", false},
		{"peering", false},
		{"stale+active+clean", false}, // "stale" is a problem flag even though it's active+clean
	}
	for _, c := range cases {
		if got := (PG{State: c.state}).Healthy(DefaultPGProblemFlags); got != c.want {
			t.Errorf("Healthy(%q) = %v, want %v", c.state, got, c.want)
		}
	}
}

// TestPGHealthyCustomFlags verifies an overridden flag list is honoured: a flag
// not in the list no longer marks a PG as a problem, and a custom flag does.
func TestPGHealthyCustomFlags(t *testing.T) {
	// Only "backfill_toofull" is a problem here; inconsistent is not in the list.
	custom := []string{"backfill_toofull"}
	if (PG{State: "active+clean+inconsistent"}).Healthy(custom) != true {
		t.Error("with a custom list omitting 'inconsistent', that PG should read healthy")
	}
	if (PG{State: "active+clean+backfill_toofull"}).Healthy(custom) != false {
		t.Error("a custom flag should mark the PG as a problem")
	}
}
