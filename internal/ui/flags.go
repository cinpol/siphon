package ui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cinpol/siphon/internal/service"
	"github.com/cinpol/siphon/internal/ui/components"
	"github.com/cinpol/siphon/internal/ui/styles"
)

// flagKeyMap holds the cluster-flags view's bindings.
type flagKeyMap struct {
	Nav     key.Binding
	Enter   key.Binding // open the flag explanation
	Enable  key.Binding // set the selected flag
	Disable key.Binding // unset the selected flag
	Filter  key.Binding
	Back    key.Binding
}

func defaultFlagKeys() flagKeyMap {
	return flagKeyMap{
		Nav:     key.NewBinding(key.WithKeys("up", "down", "j", "k"), key.WithHelp("↑/↓", "navigate")),
		Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "info")),
		Enable:  key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "enable")),
		Disable: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "disable")),
		Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
}

// flagModel is the cluster-flags view: a table of known flags with their state,
// toggled via a y/N confirmation, with an info overlay for why/risks.
type flagModel struct {
	svc       *service.Service
	keys      flagKeyMap
	table     table.Model
	catalogue []service.FlagInfo
	set       map[string]bool
	err       error
	loading   bool

	info       bool
	confirming bool
	confirm    components.Confirm
	filter     tableFilter
	pendingOp  service.Operation
	opErr      error

	width  int
	height int
}

func newFlagModel(svc *service.Service) flagModel {
	t := table.New(table.WithColumns(flagColumns()), table.WithFocused(true))
	t.SetStyles(osdTableStyles())
	return flagModel{
		svc:       svc,
		keys:      defaultFlagKeys(),
		table:     t,
		catalogue: svc.FlagCatalogue(),
		set:       map[string]bool{},
		filter:    newTableFilter(),
		loading:   true,
	}
}

type (
	flagsMsg   struct{ set []string }
	flagErrMsg struct{ err error }
)

func (m flagModel) fetch() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		set, err := m.svc.Flags(ctx)
		if err != nil {
			return flagErrMsg{err}
		}
		return flagsMsg{set}
	}
}

func (m flagModel) capturing() bool { return m.confirming || m.filter.typing() }

func (m flagModel) title() string { return "Flags" }

func (m flagModel) filterPrompt() string {
	if !m.filter.visible() {
		return ""
	}
	return m.filter.prompt()
}

func (m flagModel) supportsFilter() bool { return true }

func (m flagModel) actions() []Action {
	return []Action{act(m.keys.Enable, false), act(m.keys.Disable, false)}
}

func (m *flagModel) setSize(width, height int) {
	m.width, m.height = width, height
	m.table.SetColumns(fitColumns(flagColumns(), width))
	if height > 2 {
		m.table.SetHeight(height - 1)
	}
}

func (m flagModel) Update(msg tea.Msg) (flagModel, tea.Cmd) {
	switch msg := msg.(type) {
	case flagsMsg:
		m.err = nil
		m.opErr = nil // a fresh, successful load clears any stale op error
		m.loading = false
		m.set = map[string]bool{}
		for _, f := range msg.set {
			m.set[f] = true
		}
		m.table.SetRows(m.filter.apply(m.rows()))
		return m, nil
	case flagErrMsg:
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

	if m.confirming {
		var cmd tea.Cmd
		m.confirm, cmd = m.confirm.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m flagModel) handleKey(msg tea.KeyMsg) (flagModel, tea.Cmd) {
	if m.filter.typing() {
		cmd, _ := m.filter.handleKey(msg, &m.table, m.rows())
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

	if m.info {
		if key.Matches(msg, m.keys.Back) {
			m.info = false
		}
		return m, nil
	}

	// Enter opens the flag explanation; `e`/`d` enable/disable (via confirmation).
	switch {
	case key.Matches(msg, m.keys.Filter):
		return m, m.filter.open()
	case key.Matches(msg, m.keys.Enter):
		if _, ok := m.selected(); ok {
			m.info = true
		}
		return m, nil
	case key.Matches(msg, m.keys.Enable):
		return m.setFlag(true)
	case key.Matches(msg, m.keys.Disable):
		return m.setFlag(false)
	case key.Matches(msg, m.keys.Back):
		if m.filter.applied() {
			m.filter.clear()
			m.table.SetRows(m.filter.apply(m.rows()))
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// setFlag enables (set) or disables (unset) the selected flag via a y/n
// confirmation. If the flag is already in the requested state it is a no-op, so
// pressing `e` on an already-set flag (or `d` on an unset one) does nothing.
func (m flagModel) setFlag(enable bool) (flagModel, tea.Cmd) {
	info, ok := m.selected()
	if !ok {
		return m, nil
	}
	if m.set[info.Name] == enable {
		return m, nil
	}
	var op service.Operation
	if enable {
		op = m.svc.FlagSet(info.Name)
	} else {
		op = m.svc.FlagUnset(info.Name)
	}
	m.pendingOp = op
	// Flags are reversible, so a y/N confirmation (per the project decision).
	m.confirm = components.NewYesNo(op.Title, op.Command, op.Consequence, false)
	m.confirming = true
	return m, nil
}

func (m flagModel) View(width, height int) string {
	page := m.pageView(width)
	if m.confirming {
		return overlayCenter(page, m.confirm.View(width), width, height)
	}
	if m.info {
		if info, ok := m.selected(); ok {
			return overlayCenter(page, m.infoView(info, width), width, height)
		}
	}
	return page
}

func (m flagModel) pageView(width int) string {
	prefix := ""
	if m.opErr != nil {
		prefix = styles.Danger.Render("operation failed: "+m.opErr.Error()) + "\n\n"
	}
	if m.err != nil {
		prefix += staleBanner(m.err) + "\n\n"
	}
	if len(m.catalogue) == 0 {
		return prefix + styles.Faint.Render("Loading flags…")
	}
	return prefix + m.table.View()
}

func (m flagModel) infoView(info service.FlagInfo, width int) string {
	var b strings.Builder
	state := "unset"
	if m.set[info.Name] {
		state = "SET"
	}
	b.WriteString(styles.PanelTitle.Render(info.Name) + "  " + styles.Faint.Render("("+state+")") + "\n\n")
	b.WriteString(styles.Label.Render("What") + "  " + info.Description + "\n")
	b.WriteString(styles.Label.Render("Why ") + "  " + styles.Faint.Render(info.Why) + "\n")
	b.WriteString(styles.Label.Render("Risk") + "  " + styles.Danger.Render(info.Risk) + "\n\n")
	b.WriteString(styles.Faint.Render("[esc] back"))
	return crushBox(b.String(), width) // reuse the shared bordered-box helper
}

func (m flagModel) selected() (service.FlagInfo, bool) {
	if i, ok := m.filter.source(m.table.Cursor()); ok && i < len(m.catalogue) {
		return m.catalogue[i], true
	}
	return service.FlagInfo{}, false
}

func flagColumns() []table.Column {
	return []table.Column{
		{Title: "FLAG", Width: 14},
		{Title: "STATE", Width: 6},
		{Title: "DESCRIPTION", Width: 52},
	}
}

func (m flagModel) rows() []table.Row {
	rows := make([]table.Row, 0, len(m.catalogue))
	for _, f := range m.catalogue {
		state := "—"
		if m.set[f.Name] {
			state = "SET"
		}
		rows = append(rows, table.Row{f.Name, state, f.Description})
	}
	return rows
}
