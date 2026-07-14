// Package views renders individual screens of the TUI.
//
// A view is a pure function from domain state to a string. Keeping rendering
// free of side effects and cluster access makes screens easy to reason about
// and test, and keeps all I/O in the transport/service layers.
package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/cinpol/siphon/internal/model"
	"github.com/cinpol/siphon/internal/ui/components"
	"github.com/cinpol/siphon/internal/ui/format"
	"github.com/cinpol/siphon/internal/ui/styles"
)

// minTwoColWidth is the terminal width below which the grid collapses to a
// single column so panels stay readable on narrow terminals.
const minTwoColWidth = 64

// panelInnerHeight is the shared content height (excluding border/title) for
// the top grid panels, so a row of panels aligns cleanly.
const panelInnerHeight = 3

// Dashboard renders the cluster overview as a responsive grid of panels.
func Dashboard(d *model.Dashboard, width int) string {
	if width < 20 {
		width = 20
	}

	twoCol := width >= minTwoColWidth
	panelWidth := width - 2 // single column: one border pair
	if twoCol {
		// Two panels + one-column gap + two border pairs.
		panelWidth = (width - 1 - 4) / 2
	}
	if panelWidth < 16 {
		panelWidth = 16
	}

	capacityContent := capacityBody(d.Capacity, panelWidth)
	if !d.SectionOK("capacity") {
		capacityContent = unavailableBody()
	}

	health := components.Panel("Health", healthBody(d.Health), panelWidth, panelInnerHeight)
	capacity := components.Panel("Capacity", capacityContent, panelWidth, panelInnerHeight)
	io := components.Panel("Client IO", ioBody(d.IO), panelWidth, panelInnerHeight)
	recovery := components.Panel("Recovery", recoveryBody(d.Recovery, panelWidth), panelWidth, panelInnerHeight)

	var grid string
	if twoCol {
		grid = lipgloss.JoinVertical(lipgloss.Left,
			components.Row(health, capacity),
			components.Row(io, recovery),
		)
	} else {
		grid = lipgloss.JoinVertical(lipgloss.Left, health, capacity, io, recovery)
	}

	flagsWidth := lipgloss.Width(grid) - 2
	if flagsWidth < panelWidth {
		flagsWidth = panelWidth
	}
	flagsContent := flagsBody(d.Flags)
	if !d.SectionOK("flags") {
		flagsContent = unavailableBody()
	}
	flags := components.Panel("Flags", flagsContent, flagsWidth, 1)

	sections := []string{grid}
	// When the cluster has active health checks, expand them into a full-width
	// detail section (code, summary and per-item detail lines) below the grid —
	// the compact Health tile above only has room for the codes.
	if detail := healthDetailPanel(d.Health, flagsWidth); detail != "" {
		sections = append(sections, detail)
	}
	sections = append(sections, flags)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// healthPreviewLines caps how many health-detail lines the dashboard shows
// inline. Real clusters can emit hundreds of detail items (e.g. a backlog of
// pending scrubs during recovery); rendering them all would overflow the panel
// and push the Flags section off-screen. When the output is longer than this
// budget the inline panel shows a preview and a hint, and the full text lives in
// the scrollable Health-detail overlay (Enter). This keeps the dashboard a
// bounded overview regardless of cluster size.
const healthPreviewLines = 8

// HealthDetailLines renders the full `ceph health detail` content as styled
// lines: for each active check its code + severity, its one-line summary, and
// each per-item detail line. It is the single source of truth for both the
// inline dashboard preview and the scrollable Health-detail overlay, so the two
// can never drift apart.
func HealthDetailLines(h model.Health) []string {
	lines, _ := healthLines(h, 0)
	return lines
}

// healthLines builds the health-detail lines, styling at most limit of them
// (limit <= 0 means all) while always returning the true total count. The limit
// lets the dashboard's inline preview avoid styling hundreds of detail lines it
// will never show: on a busy cluster the panel needs only the first few lines
// plus a count, and this is re-rendered on every frame, so styling the whole
// backlog each time was pure waste. The overlay asks for all lines (limit 0),
// but its content is set only when it opens/refreshes, not per keystroke.
func healthLines(h model.Health, limit int) (lines []string, total int) {
	// add records one line, styling it only while we are still under the limit.
	// build is a closure so the (possibly expensive) styling isn't evaluated for
	// lines beyond the limit — it is only called when the line will be kept.
	add := func(build func() string) {
		total++
		if limit <= 0 || len(lines) < limit {
			lines = append(lines, build())
		}
	}
	for _, c := range h.Checks {
		add(func() string {
			marker := styles.Health(model.HealthStatus(c.Severity)).Render("●")
			return fmt.Sprintf("%s %s  %s", marker, c.Code, styles.Faint.Render("("+c.Severity+")"))
		})
		if c.Summary != "" {
			add(func() string { return "    " + c.Summary })
		}
		for _, d := range c.Details {
			// Some checks repeat the summary verbatim as their only detail
			// (e.g. OSDMAP_FLAGS); don't echo it twice.
			if d == c.Summary {
				continue
			}
			add(func() string { return styles.Faint.Render("      " + d) })
		}
	}
	return lines, total
}

// healthDetailPanel renders the expanded, full-width health section shown when
// the cluster has active checks. It returns "" for a healthy cluster (no
// checks), so the section only appears when there is something to show. When the
// content exceeds healthPreviewLines it is truncated to a preview with a hint
// pointing at the scrollable overlay; short output is shown in full (so there is
// no regression when there are only a few items).
func healthDetailPanel(h model.Health, width int) string {
	if len(h.Checks) == 0 {
		return ""
	}
	// Style at most healthPreviewLines; total is the full count (used for the
	// "+N more" hint) but costs nothing beyond the budget.
	lines, total := healthLines(h, healthPreviewLines)
	if total > healthPreviewLines {
		hidden := total - (healthPreviewLines - 1)
		lines = append(lines[:healthPreviewLines-1:healthPreviewLines-1],
			styles.Faint.Render(fmt.Sprintf("      +%d more — press enter to view", hidden)))
	}
	return components.Panel("Health detail", strings.Join(lines, "\n"), width, len(lines))
}

// unavailableBody is shown in a panel whose data failed to load this cycle.
func unavailableBody() string {
	return styles.Faint.Render("unavailable — will retry next refresh")
}

func healthBody(h model.Health) string {
	var b strings.Builder
	b.WriteString(styles.Health(h.Status).Render(string(h.Status)))
	if len(h.Checks) == 0 {
		b.WriteString("\n" + styles.Faint.Render("no active checks"))
		return b.String()
	}
	for _, c := range h.Checks {
		marker := styles.Health(model.HealthStatus(c.Severity)).Render("●")
		b.WriteString(fmt.Sprintf("\n%s %s", marker, c.Code))
	}
	return b.String()
}

func capacityBody(c model.Capacity, panelWidth int) string {
	ratio := c.UsedRatio()
	head := fmt.Sprintf("%s / %s  %s",
		format.Bytes(c.UsedBytes),
		format.Bytes(c.TotalBytes),
		styles.Utilization(ratio).Render(format.Percent(ratio)),
	)
	meter := components.Meter(ratio, meterWidth(panelWidth))
	avail := styles.Faint.Render("avail " + format.Bytes(c.AvailBytes))
	return head + "\n" + meter + "\n" + avail
}

func ioBody(io model.ClientIO) string {
	read := fmt.Sprintf("read   %-11s %s",
		format.Rate(io.ReadBytesSec), styles.Faint.Render(fmt.Sprintf("%d op/s", io.ReadOpsSec)))
	write := fmt.Sprintf("write  %-11s %s",
		format.Rate(io.WriteBytesSec), styles.Faint.Render(fmt.Sprintf("%d op/s", io.WriteOpsSec)))
	return read + "\n" + write
}

func recoveryBody(r model.Recovery, panelWidth int) string {
	if !r.Active() {
		clean := fmt.Sprintf("%d/%d PGs active+clean", r.CleanPGs, r.TotalPGs)
		return styles.Health(model.HealthOK).Render("idle") + "\n" + styles.Faint.Render(clean)
	}

	var b strings.Builder
	if r.RecoveringBytesSec > 0 {
		b.WriteString("recovering " + format.Rate(r.RecoveringBytesSec) + "\n")
	}
	b.WriteString(fmt.Sprintf("misplaced %s  degraded %s", format.Percent(r.MisplacedRatio), format.Percent(r.DegradedRatio)))
	if r.TotalPGs > 0 {
		cleanRatio := float64(r.CleanPGs) / float64(r.TotalPGs)
		b.WriteString(fmt.Sprintf("\n%d/%d clean ", r.CleanPGs, r.TotalPGs) + components.Bar(cleanRatio, meterWidth(panelWidth)/2))
	}
	return b.String()
}

func flagsBody(flags []string) string {
	if len(flags) == 0 {
		return styles.Faint.Render("none set")
	}
	return strings.Join(flags, "  ")
}

// meterWidth derives a sensible bar width from the panel width.
func meterWidth(panelWidth int) int {
	w := panelWidth - 2
	switch {
	case w < 8:
		return 8
	case w > 40:
		return 40
	default:
		return w
	}
}
