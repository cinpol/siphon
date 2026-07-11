package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/cinpol/argonaut/internal/model"
	"github.com/cinpol/argonaut/internal/ui/styles"
	"github.com/cinpol/argonaut/internal/version"
)

// logoLines is the Argonaut ASCII wordmark shown at the top-left of the header.
// Raw-string segments keep the backslashes literal; the backticks in the art are
// spliced in as double-quoted strings.
var logoLines = []string{
	`   ___                                __ `,
	`  / _ | _______ ____  ___  ___ ___ __/ /_`,
	` / __ |/ __/ _ ` + "`" + `/ _ \/ _ \/ _ ` + "`" + `/ // / __/`,
	`/_/ |_/_/  \_, /\___/_//_/\_,_/\_,_/\__/ `,
	`          /___/                          `,
}

// headerRows is the header block's height: every column is padded to it, and
// action lists longer than this wrap into an additional column (so e.g. CRUSH's
// seven actions form two columns rather than a taller header).
const headerRows = 5

// navColWidth is the fixed width of the first navigation column, so the second
// column lines up regardless of label lengths.
const navColWidth = 17

// navItem is one page shortcut shown in the navigation columns.
type navItem struct {
	key   string
	label string
	id    viewID
}

var navItems = []navItem{
	{"1", "Dashboard", viewDashboard},
	{"2", "OSDs", viewOSD},
	{"3", "Pools", viewPool},
	{"4", "CRUSH", viewCrush},
	{"5", "Flags", viewFlags},
	{"6", "Services", viewServices},
	{"7", "PGs", viewPGs},
}

// frame is the single layout manager: it arranges every screen identically as
// separator → header → full-size titled workspace panel (→ command prompt).
// Views supply only their inner content; no view renders its own chrome.
func (m Model) frame() string {
	sep := m.separator()
	header := m.headerBlock()

	// A single prompt line may sit just above the workspace panel: the ":" command
	// prompt when it is open, otherwise the active view's "/" filter line. They
	// share the slot because they are never entered at the same time.
	var promptLine string
	if m.cmdActive {
		promptLine = lipgloss.NewStyle().PaddingLeft(1).Render(m.cmd.View())
	} else if v := m.activeView(); v != nil {
		if p := v.filterPrompt(); p != "" {
			promptLine = lipgloss.NewStyle().PaddingLeft(1).Render(p)
		}
	}

	panelH := m.height - lipgloss.Height(sep) - lipgloss.Height(header)
	if promptLine != "" {
		panelH -= lipgloss.Height(promptLine)
	}
	if panelH < 3 {
		panelH = 3
	}

	innerW := m.width - 2
	if innerW < 1 {
		innerW = 1
	}
	innerH := panelH - 2
	if innerH < 1 {
		innerH = 1
	}

	var body string
	if v := m.activeView(); v != nil {
		body = v.View(innerW, innerH)
	} else {
		body = m.dashboardBody(innerW)
	}
	// Views composite their own popups (details/form/confirm) over the page with
	// overlayCenter, so the shell no longer centres anything here — it just places
	// the returned body in the panel.

	panel := workspacePanel(m.viewTitle(), body, m.width, panelH)

	parts := []string{sep, header}
	if promptLine != "" {
		parts = append(parts, promptLine)
	}
	parts = append(parts, panel)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// separator is the full-width green rule at the very top.
func (m Model) separator() string {
	w := m.width
	if w < 1 {
		w = 1
	}
	return styles.Separator.Render(strings.Repeat("─", w))
}

// viewTitle is the page name shown in the workspace panel's top border.
func (m Model) viewTitle() string {
	if v := m.activeView(); v != nil {
		return v.title()
	}
	return "Dashboard"
}

// headerBlock arranges the header as left-aligned columns: cluster info, the two
// navigation columns, the dynamic action columns, and the global-keys column,
// with the decorative logo prepended when the terminal is wide enough to fit it
// without squeezing the functional columns. The whole block is indented one
// space (to line up with the panel border below) and capped to the terminal
// width so an over-wide header clips rather than wrapping.
func (m Model) headerBlock() string {
	const gap = "   "

	cols := []string{m.clusterInfoColumn(), m.navColumns()}
	if ac := m.actionColumns(); ac != "" {
		cols = append(cols, ac)
	}
	cols = append(cols, m.globalColumn())

	body := cols[0]
	for _, c := range cols[1:] {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, gap, c)
	}

	// The logo is decorative; only show it if the functional columns still fit
	// beside it. On narrower terminals it is dropped so actions stay visible.
	logo := m.logoColumn()
	if m.width == 0 || lipgloss.Width(logo)+len(gap)+lipgloss.Width(body) <= m.width-1 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, logo, gap, body)
	}

	out := lipgloss.NewStyle().PaddingLeft(1).Render(body)
	if m.width > 0 {
		out = lipgloss.NewStyle().MaxWidth(m.width).Render(out)
	}
	return out
}

// headerHeight is the number of rows the header occupies, used to size the
// workspace panel consistently in frame and when sizing sub-views on resize.
func (m Model) headerHeight() int {
	return lipgloss.Height(m.headerBlock())
}

// logoColumn is the leftmost column: the ASCII "Argonaut" wordmark in orange.
func (m Model) logoColumn() string {
	return padColumn(styles.HeaderKey.Render(strings.Join(logoLines, "\n")))
}

// clusterInfoColumn sits directly to the right of the logo: the cluster
// identity/health fields (orange keys, bold-white values).
func (m Model) clusterInfoColumn() string {
	ctx, release := "connecting…", "…"
	status := model.HealthUnknown
	if m.dash != nil {
		ctx = shortID(m.dash.FSID)
		status = m.dash.Health.Status
		if r := m.dash.Version.Release; r != "" {
			release = r
		}
	}
	updated := "—"
	if !m.lastSync.IsZero() {
		updated = m.lastSync.Format("15:04:05")
	}
	if m.dashLoading {
		updated += " ⟳"
	}

	field := func(k, v string) string {
		return styles.HeaderKey.Render(fmt.Sprintf("%-9s", k+":")) + " " + styles.HeaderValue.Render(v)
	}
	statusRow := styles.HeaderKey.Render(fmt.Sprintf("%-9s", "Status:")) + " " +
		styles.Health(status).Bold(true).Render(string(status))

	// The Argonaut row shows the running application version. On the mock it also
	// carries a prominent warning badge so a session on demo data can never be
	// mistaken for a live cluster. The badge is kept short ("MOCK") so it doesn't
	// widen the header column enough to clip the action labels.
	appVal := styles.HeaderValue.Render(version.Version)
	if m.clientName == "mock" {
		appVal += "  " + styles.MockBadge.Render("MOCK")
	}
	appRow := styles.HeaderKey.Render(fmt.Sprintf("%-9s", "Argonaut:")) + " " + appVal

	fields := lipgloss.JoinVertical(lipgloss.Left,
		field("Context", ctx),
		appRow,
		statusRow,
		field("Version", release),
		field("Updated", updated),
	)
	return padColumn(fields)
}

// navColumns renders pages 1–5 and 6–7 as two navigation columns.
func (m Model) navColumns() string {
	colA := m.navBlock(navItems[:5])
	colB := m.navBlock(navItems[5:])
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(navColWidth).Render(colA), colB)
}

func (m Model) navBlock(items []navItem) string {
	rows := make([]string, 0, len(items))
	for _, it := range items {
		label := styles.NavLabel.Render(it.label)
		if it.id == m.view {
			label = styles.NavActive.Render(it.label)
		}
		rows = append(rows, styles.NavKey.Render("<"+it.key+">")+" "+label)
	}
	return padColumn(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

// actionColumns renders the active view's actions, generated dynamically and
// wrapped into as many columns as needed (headerRows actions per column). Esc
// (back) is navigation, not a quick action, so it is omitted.
func (m Model) actionColumns() string {
	var actions []Action
	if v := m.activeView(); v != nil {
		actions = v.actions()
	}
	visible := make([]Action, 0, len(actions))
	for _, a := range actions {
		if a.Key == "esc" {
			continue
		}
		visible = append(visible, a)
	}
	if len(visible) == 0 {
		return ""
	}

	var cols []string
	for i := 0; i < len(visible); i += headerRows {
		end := i + headerRows
		if end > len(visible) {
			end = len(visible)
		}
		rows := make([]string, 0, end-i)
		for _, a := range visible[i:end] {
			rows = append(rows, styles.ActionKey.Render("<"+a.Key+">")+" "+styles.ActionLabel.Render(a.Label))
		}
		cols = append(cols, padColumn(lipgloss.JoinVertical(lipgloss.Left, rows...)))
	}

	gap := "   "
	out := cols[0]
	for _, c := range cols[1:] {
		out = lipgloss.JoinHorizontal(lipgloss.Top, out, gap, c)
	}
	return out
}

// globalColumn shows the always-available keys that used to live in the footer.
// Its shortcut keys use the same blue as the nav/action keys for consistency.
// The "/" filter is a global affordance too, so it lives here (right after
// Command) rather than among a view's context actions — but only on views that
// actually support filtering.
func (m Model) globalColumn() string {
	item := func(k, l string) string {
		return styles.ActionKey.Render("<"+k+">") + " " + styles.ActionLabel.Render(l)
	}
	rows := []string{item(":", "Command")}
	if v := m.activeView(); v != nil && v.supportsFilter() {
		rows = append(rows, item("/", "Filter"))
	}
	rows = append(rows, item("q", "Quit"))
	return padColumn(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

// padColumn pads a header column to the fixed header height so all columns align.
func padColumn(s string) string {
	return lipgloss.NewStyle().Height(headerRows).Render(s)
}

// workspacePanel draws the primary workspace: a baby-blue rounded rectangle of
// exactly width × height (never shrinking to content), with the page title
// centred and bold-white embedded in the top border.
func workspacePanel(title, body string, width, height int) string {
	bs := lipgloss.NewStyle().Foreground(styles.BabyBlueColor())
	innerW := width - 2
	innerH := height - 2
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}

	// Normalise the body to exactly innerH lines, each innerW cells wide.
	block := lipgloss.NewStyle().Width(innerW).MaxWidth(innerW).Height(innerH).MaxHeight(innerH).Render(body)
	lines := strings.Split(block, "\n")

	var b strings.Builder
	b.WriteString(topBorder(title, innerW, bs) + "\n")
	for i := 0; i < innerH; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		b.WriteString(bs.Render("│") + padTo(line, innerW) + bs.Render("│") + "\n")
	}
	b.WriteString(bs.Render("└" + strings.Repeat("─", innerW) + "┘"))
	return b.String()
}

// topBorder builds the panel's top edge with the title centred inside it.
//
// Square corners (┌ ┐) are used deliberately: rounded corners curve inward and
// render a few pixels short of the cell edge, so the panel would look narrower
// than the full-width top separator and sit slightly right of the flush-left
// header. Square corners reach the cell edges and line up with both.
func topBorder(title string, innerW int, bs lipgloss.Style) string {
	label := " " + title + " "
	if title == "" || lipgloss.Width(label) > innerW {
		return bs.Render("┌" + strings.Repeat("─", innerW) + "┐")
	}
	tw := lipgloss.Width(label)
	left := (innerW - tw) / 2
	right := innerW - tw - left
	mid := " " + styles.WorkspaceTitle.Render(title) + " "
	return bs.Render("┌"+strings.Repeat("─", left)) + mid + bs.Render(strings.Repeat("─", right)+"┐")
}

// padTo right-pads a (possibly styled) line with spaces to exactly w cells.
func padTo(s string, w int) string {
	n := lipgloss.Width(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}
