// Package components holds reusable TUI building blocks shared across views.
//
// A component is a pure render helper: values in, string out. Keeping them free
// of state and I/O makes the views easy to compose and reason about. As the app
// grows, richer stateful widgets (tables, trees, the confirm-modal) will join
// these, but the render-only helpers stay here.
package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/cinpol/siphon/internal/ui/styles"
)

// Panel renders a bordered box with a title and body. width and innerHeight are
// the target content dimensions (excluding the border); the body is padded or
// truncated to innerHeight lines so panels laid out in a row share a common
// height and align cleanly into a grid.
func Panel(title, body string, width, innerHeight int) string {
	lines := strings.Split(body, "\n")
	if len(lines) > innerHeight {
		lines = lines[:innerHeight]
	}
	for len(lines) < innerHeight {
		lines = append(lines, "")
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		styles.PanelTitle.Render(title),
		strings.Join(lines, "\n"),
	)

	return styles.PanelBorder.Width(width).Render(content)
}

// Row joins panels side by side with a single-column gap between them.
func Row(panels ...string) string {
	if len(panels) == 0 {
		return ""
	}
	withGaps := make([]string, 0, len(panels)*2-1)
	for i, p := range panels {
		if i > 0 {
			withGaps = append(withGaps, " ")
		}
		withGaps = append(withGaps, p)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, withGaps...)
}
