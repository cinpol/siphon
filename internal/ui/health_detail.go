package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cinpol/siphon/internal/ui/styles"
	"github.com/cinpol/siphon/internal/ui/views"
)

// The Health-detail overlay makes the dashboard's `ceph health detail` output
// scrollable. The dashboard tile and its inline panel only have room for a
// preview, but a busy cluster can emit hundreds of detail lines (e.g. a backlog
// of pending scrubs). Pressing Enter on the dashboard opens this overlay — a
// scrollable viewport composited over the page — and Esc closes it.
//
// The dashboard is not a resourceView (it is a pure render off the Model), so
// its one stateful interaction lives here rather than in a sub-model.

// healthDetailHasContent reports whether there is any health-detail output to
// show, i.e. the cluster has active checks. Enter only opens the overlay when
// this is true, so a healthy cluster's dashboard stays inert.
func (m Model) healthDetailHasContent() bool {
	return m.dash != nil && len(m.dash.Health.Checks) > 0
}

// dashboardActions are the dashboard's context actions for the header action
// bar. The dashboard is not a resourceView, so it supplies its actions here
// instead of through actions(). The Health-detail action only appears when there
// is something to scroll into.
func (m Model) dashboardActions() []Action {
	if m.healthDetailHasContent() {
		return []Action{{Key: "enter", Label: "Health detail"}}
	}
	return nil
}

// openHealthDetail opens the overlay, loads the full health-detail content into
// the viewport and resets the scroll position to the top.
func (m *Model) openHealthDetail() {
	m.healthDetail = true
	m.loadHealthDetail()
	m.healthVP.GotoTop()
}

// closeHealthDetail dismisses the overlay and drops the cached background.
func (m *Model) closeHealthDetail() {
	m.healthDetail = false
	m.dashBG = ""
}

// loadHealthDetail sizes the viewport to the overlay's interior, (re)loads its
// content, and refreshes the cached dashboard background. It is called when the
// overlay opens, on a terminal resize while it is open, and when a refresh brings
// new health data — exactly the moments the geometry, the text, or the
// background can change; between them (e.g. during scrolling) nothing here reruns.
func (m *Model) loadHealthDetail() {
	w, h := m.healthOverlayInner()
	m.healthVP.Width, m.healthVP.Height = w, h
	if m.dash != nil {
		m.healthVP.SetContent(strings.Join(views.HealthDetailLines(m.dash.Health), "\n"))
	}
	m.dashBG = m.renderDashboard()
}

// handleHealthDetailKey routes keys while the overlay is open. Esc closes it;
// g/G (and Home/End) jump to the top/bottom; ctrl+c stays a safety quit; every
// other key is forwarded to the viewport, which handles ↑/↓, PgUp/PgDn and the
// half-page keys. While the overlay is open it captures input, so the number
// hotkeys don't switch views out from under it (Esc first, as elsewhere).
func (m Model) handleHealthDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeHealthDetail()
		return m, nil
	case "g", "home":
		m.healthVP.GotoTop()
		return m, nil
	case "G", "end":
		m.healthVP.GotoBottom()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.healthVP, cmd = m.healthVP.Update(msg)
	return m, cmd
}

// healthOverlayBox returns the overlay box's outer width and height. The box
// fills most of the dashboard panel's interior, leaving a margin so the page
// stays visible around it, and is capped so it doesn't stretch uncomfortably
// wide on large terminals.
func (m Model) healthOverlayBox() (outerW, outerH int) {
	iw, ih := m.contentWidth(), m.contentHeight()

	// Width is proportional so it grows with the terminal (widescreens get a
	// genuinely large popup), capped so short detail lines don't stretch
	// edge-to-edge on ultrawide monitors and floored so it stays usable on narrow
	// terminals. The result can never exceed the interior actually available.
	outerW = iw * 80 / 100
	if outerW > 140 {
		outerW = 140
	}
	if outerW < 40 {
		outerW = 40
	}
	if outerW > iw {
		outerW = iw
	}

	outerH = ih - 2
	if outerH < 6 {
		outerH = 6
	}
	return outerW, outerH
}

// healthOverlayInner returns the viewport's interior size: the box minus its
// rounded border (2 columns / 2 rows), horizontal padding (2 columns) and the
// title + hint rows (2 rows).
func (m Model) healthOverlayInner() (w, h int) {
	ow, oh := m.healthOverlayBox()
	w, h = ow-4, oh-4
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}

// healthDetailView renders the overlay box: a titled, baby-blue bordered panel
// wrapping the scrollable viewport, with a footer showing the scroll keys and
// the current scroll position.
func (m Model) healthDetailView() string {
	ow, _ := m.healthOverlayBox()
	innerW, _ := m.healthOverlayInner()

	title := styles.WorkspaceTitle.Render("Health detail")
	content := lipgloss.JoinVertical(lipgloss.Left, title, m.healthVP.View(), m.healthScrollHint(innerW))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.BabyBlueColor()).
		Padding(0, 1).
		Width(ow - 2). // Width is the content box; the border adds the other 2 columns.
		Render(content)
}

// healthScrollHint is the overlay's footer: the scroll/close key hints on the
// left and the scroll position on the right, so it is always discoverable that
// the content is scrollable and how far through it the operator is.
func (m Model) healthScrollHint(width int) string {
	left := styles.Faint.Render("↑/↓ pgup/pgdn g/G scroll · esc close")

	pos := "all"
	if m.healthVP.TotalLineCount() > m.healthVP.Height {
		pos = fmt.Sprintf("%d%%", int(m.healthVP.ScrollPercent()*100))
	}
	right := styles.Faint.Render(pos)

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}
