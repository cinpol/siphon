// Package format contains small, pure formatting helpers shared by the views.
//
// They live in one place so byte and percentage rendering is consistent across
// every panel and easy to unit-test.
package format

import "fmt"

const (
	kib = 1024
	mib = kib * 1024
	gib = mib * 1024
	tib = gib * 1024
	pib = tib * 1024
)

// Bytes renders a byte count using IEC units (KiB, MiB, …), matching how Ceph
// itself reports capacity.
func Bytes(n int64) string {
	switch {
	case n >= pib:
		return fmt.Sprintf("%.1f PiB", float64(n)/pib)
	case n >= tib:
		return fmt.Sprintf("%.1f TiB", float64(n)/tib)
	case n >= gib:
		return fmt.Sprintf("%.1f GiB", float64(n)/gib)
	case n >= mib:
		return fmt.Sprintf("%.1f MiB", float64(n)/mib)
	case n >= kib:
		return fmt.Sprintf("%.1f KiB", float64(n)/kib)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// Rate renders a per-second byte rate, e.g. "120.0 MiB/s".
func Rate(n int64) string {
	return Bytes(n) + "/s"
}

// Percent renders a 0..1 ratio as a whole-number percentage.
func Percent(ratio float64) string {
	return fmt.Sprintf("%.0f%%", ratio*100)
}

// Count renders a large item count compactly, e.g. 52400 -> "52.4k".
func Count(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
