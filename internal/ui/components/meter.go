package components

import (
	"math"
	"strings"

	"github.com/cinpol/argonaut/internal/ui/styles"
)

// Meter renders a horizontal utilisation bar for a 0..1 ratio, coloured by
// fullness (green/orange/red). width is the total number of cells in the bar.
func Meter(ratio float64, width int) string {
	if width < 1 {
		width = 1
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	filled := int(math.Round(ratio * float64(width)))
	if filled > width {
		filled = width
	}

	bar := styles.Utilization(ratio).Render(strings.Repeat("█", filled))
	rest := styles.Faint.Render(strings.Repeat("░", width-filled))
	return bar + rest
}

// Bar renders a neutral (accent-coloured) progress bar for a 0..1 ratio. Unlike
// Meter, it carries no good/bad semantics, so it suits quantities where "full"
// is not inherently bad — e.g. the share of PGs that are active+clean.
func Bar(ratio float64, width int) string {
	if width < 1 {
		width = 1
	}
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}

	filled := int(math.Round(ratio * float64(width)))
	if filled > width {
		filled = width
	}

	bar := styles.Accent.Render(strings.Repeat("█", filled))
	rest := styles.Faint.Render(strings.Repeat("░", width-filled))
	return bar + rest
}
