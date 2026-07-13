package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cinpol/siphon/internal/model"
	"github.com/cinpol/siphon/internal/service"
	"github.com/cinpol/siphon/internal/ui/components"
	"github.com/cinpol/siphon/internal/ui/format"
	"github.com/cinpol/siphon/internal/ui/styles"
)

type pgKeyMap struct {
	Nav       key.Binding
	Enter     key.Binding
	Filter    key.Binding
	Problems  key.Binding
	Scrub     key.Binding
	DeepScrub key.Binding
	Repair    key.Binding
	Back      key.Binding
}

func defaultPGKeys() pgKeyMap {
	return pgKeyMap{
		Nav:       key.NewBinding(key.WithKeys("up", "down", "j", "k"), key.WithHelp("↑/↓", "navigate")),
		Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
		Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Problems:  key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "problems only")),
		Scrub:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "scrub")),
		DeepScrub: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "deep-scrub")),
		Repair:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "repair")),
		Back:      key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
}

// pgModel is the placement-groups view: every PG in the cluster, with a text
// filter, a "problems only" toggle, a detail view, and scrub/deep-scrub/repair
// operations on the selected PG.
type pgModel struct {
	svc  *service.Service
	keys pgKeyMap

	table   table.Model
	pgs     []model.PG // all PGs
	visible []model.PG // after filter + problems toggle

	filter       string
	filterInput  textinput.Model
	filtering    bool
	problemsOnly bool
	detail       bool

	confirming bool
	confirm    components.Confirm
	pendingOp  service.Operation
	opErr      error

	err     error
	loading bool
	width   int
	height  int
}

func newPGModel(svc *service.Service) pgModel {
	t := table.New(table.WithColumns(pgColumns()), table.WithFocused(true))
	t.SetStyles(osdTableStyles())

	fi := textinput.New()
	fi.Prompt = "/"
	fi.CharLimit = 32

	return pgModel{svc: svc, keys: defaultPGKeys(), table: t, filterInput: fi, loading: true}
}

type (
	pgsMsg   struct{ pgs []model.PG }
	pgErrMsg struct{ err error }
)

// fetch loads every placement group in the cluster.
func (m pgModel) fetch() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		pgs, err := m.svc.PGs(ctx)
		if err != nil {
			return pgErrMsg{err}
		}
		return pgsMsg{pgs}
	}
}

func (m pgModel) capturing() bool { return m.filtering || m.confirming }

// filterPrompt shows the live "/" prompt while typing and, once committed, a
// compact indicator of the applied filter (there is no status line anymore).
func (m pgModel) filterPrompt() string {
	if m.filtering {
		return m.filterInput.View()
	}
	if m.filter != "" {
		return styles.NavKey.Render("/") + m.filter +
			styles.Faint.Render(fmt.Sprintf("   %d/%d · esc to clear", len(m.visible), len(m.pgs)))
	}
	return ""
}

func (m pgModel) supportsFilter() bool { return true }

func (m pgModel) title() string { return "PGs" }

func (m pgModel) actions() []Action {
	return []Action{
		act(m.keys.Problems, false),
		act(m.keys.Scrub, false),
		act(m.keys.DeepScrub, false),
		act(m.keys.Repair, false),
	}
}

func (m *pgModel) setSize(width, height int) {
	m.width, m.height = width, height
	m.table.SetColumns(fitColumns(pgColumns(), width))
	if height > 2 {
		m.table.SetHeight(height - 1)
	}
}

func (m pgModel) Update(msg tea.Msg) (pgModel, tea.Cmd) {
	switch msg := msg.(type) {
	case pgsMsg:
		m.err = nil
		m.opErr = nil // a fresh, successful load clears any stale op error
		m.loading = false
		m.pgs = msg.pgs
		m.rebuild()
		return m, nil
	case pgErrMsg:
		m.err = msg.err
		m.loading = false
		return m, nil
	case opResultMsg:
		if msg.err != nil {
			m.opErr = msg.err
			return m, nil
		}
		m.opErr = nil
		m.loading = true
		return m, m.fetch()
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m pgModel) handleKey(msg tea.KeyMsg) (pgModel, tea.Cmd) {
	if m.filtering {
		switch msg.String() {
		case "enter":
			m.filtering = false // apply and keep the filter
			return m, nil
		case "esc":
			m.filtering = false
			m.filter = ""
			m.filterInput.SetValue("")
			m.rebuild()
			return m, nil
		}
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		m.filter = m.filterInput.Value()
		m.rebuild()
		return m, cmd
	}

	if m.confirming {
		var cmd tea.Cmd
		m.confirm, cmd = m.confirm.Update(msg)
		switch m.confirm.State() {
		case components.ConfirmAccepted:
			m.confirming = false
			return m, runOp(m.pendingOp)
		case components.ConfirmCancelled:
			m.confirming = false
			return m, nil
		}
		return m, cmd
	}

	switch {
	case key.Matches(msg, m.keys.Enter):
		if !m.detail && len(m.visible) > 0 {
			m.detail = true
		}
		return m, nil
	case key.Matches(msg, m.keys.Back):
		switch {
		case m.detail:
			m.detail = false
		case m.filter != "":
			m.filter = ""
			m.filterInput.SetValue("")
			m.rebuild()
		}
		return m, nil
	}
	if m.detail {
		return m, nil
	}
	return m.dispatch(msg)
}

// dispatch runs a normal-mode action from a direct hotkey.
func (m pgModel) dispatch(msg tea.KeyMsg) (pgModel, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		return m, m.filterInput.Focus()
	case key.Matches(msg, m.keys.Problems):
		m.problemsOnly = !m.problemsOnly
		m.rebuild()
		return m, nil
	case key.Matches(msg, m.keys.Scrub):
		if pg, ok := m.selectedPG(); ok {
			return m.startConfirm(m.svc.PGScrub(pg.ID))
		}
	case key.Matches(msg, m.keys.DeepScrub):
		if pg, ok := m.selectedPG(); ok {
			return m.startConfirm(m.svc.PGDeepScrub(pg.ID))
		}
	case key.Matches(msg, m.keys.Repair):
		if pg, ok := m.selectedPG(); ok {
			return m.startConfirm(m.svc.PGRepair(pg.ID))
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m pgModel) startConfirm(op service.Operation) (pgModel, tea.Cmd) {
	m.pendingOp = op
	m.confirm = components.NewYesNo(op.Title, op.Command, op.Consequence, false)
	m.confirming = true
	return m, nil
}

// rebuild applies the problems toggle and text filter, then refreshes the table.
func (m *pgModel) rebuild() {
	filter := strings.ToLower(strings.TrimSpace(m.filter))
	m.visible = m.visible[:0]
	for _, pg := range m.pgs {
		if m.problemsOnly && pg.Healthy() {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(pg.ID), filter) && !strings.Contains(strings.ToLower(pg.State), filter) {
			continue
		}
		m.visible = append(m.visible, pg)
	}
	m.table.SetRows(pgRows(m.visible))
	if m.table.Cursor() >= len(m.visible) {
		m.table.GotoTop()
	}
}

func (m pgModel) View(width, height int) string {
	page := m.pageView(width)
	if m.confirming {
		return overlayCenter(page, m.confirm.View(width), width, height)
	}
	if m.detail {
		if pg, ok := m.selectedPG(); ok {
			return overlayCenter(page, pgDetail(pg, width), width, height)
		}
	}
	return page
}

func (m pgModel) pageView(width int) string {
	prefix := ""
	if m.opErr != nil {
		prefix = styles.Danger.Render("operation failed: "+m.opErr.Error()) + "\n\n"
	}
	if m.err != nil && len(m.pgs) == 0 {
		return prefix + errorScreen(m.err)
	}
	if m.err != nil {
		prefix += staleBanner(m.err) + "\n\n"
	}
	switch {
	case m.loading && m.pgs == nil:
		return prefix + styles.Faint.Render("Loading PGs…")
	case len(m.pgs) == 0:
		return prefix + styles.Faint.Render("No PGs reported.")
	}
	return prefix + m.table.View()
}

func (m pgModel) selectedPG() (model.PG, bool) {
	i := m.table.Cursor()
	if i >= 0 && i < len(m.visible) {
		return m.visible[i], true
	}
	return model.PG{}, false
}

func pgColumns() []table.Column {
	return []table.Column{
		{Title: "PGID", Width: 8},
		{Title: "STATE", Width: 28},
		{Title: "UP", Width: 12},
		{Title: "ACTING", Width: 12},
		{Title: "OBJECTS", Width: 8},
	}
}

func pgRows(pgs []model.PG) []table.Row {
	rows := make([]table.Row, 0, len(pgs))
	for _, pg := range pgs {
		rows = append(rows, table.Row{
			pg.ID,
			pg.State,
			intsToStr(pg.Up),
			intsToStr(pg.Acting),
			format.Count(pg.Objects),
		})
	}
	return rows
}

func pgDetail(pg model.PG, width int) string {
	body := fmt.Sprintf(
		"state         %s\nup            %s  (primary %d)\nacting        %s  (primary %d)\nobjects       %s\ndata          %s\nlast scrub    %s\nlast deep     %s",
		pg.State,
		intsToStr(pg.Up), pg.UpPrimary,
		intsToStr(pg.Acting), pg.ActingPrimary,
		format.Count(pg.Objects),
		format.Bytes(pg.Bytes),
		orDash(pg.LastScrub),
		orDash(pg.LastDeepScrub),
	)
	return components.Panel("PG "+pg.ID, body, min(width-2, 56), 8)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func intsToStr(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.Itoa(x)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
