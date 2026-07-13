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
	"github.com/charmbracelet/lipgloss"

	"github.com/cinpol/siphon/internal/model"
	"github.com/cinpol/siphon/internal/service"
	"github.com/cinpol/siphon/internal/ui/components"
	"github.com/cinpol/siphon/internal/ui/format"
	"github.com/cinpol/siphon/internal/ui/styles"
)

// osdMode is the OSD view's interaction state. Beyond normal browsing, the view
// can be capturing input for a reweight value or for an operation confirmation.
type osdMode int

const (
	osdNormal osdMode = iota
	osdWeightPrompt
	osdConfirm
)

// osdKeyMap holds the OSD view's bindings, surfaced in the shell's help.
type osdKeyMap struct {
	Nav      key.Binding
	Enter    key.Binding
	Back     key.Binding
	Filter   key.Binding
	Out      key.Binding
	In       key.Binding
	Reweight key.Binding
	Destroy  key.Binding
	Purge    key.Binding
	Remove   key.Binding
}

func defaultOSDKeys() osdKeyMap {
	return osdKeyMap{
		Nav:      key.NewBinding(key.WithKeys("up", "down", "j", "k"), key.WithHelp("↑/↓", "navigate")),
		Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Out:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "out")),
		In:       key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "in")),
		Reweight: key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "reweight")),
		Destroy:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "destroy")),
		Purge:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "purge")),
		Remove:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "remove")),
	}
}

// osdModel is the stateful OSD view: a navigable table with an on-demand detail
// overlay and the mutating-operation workflow (weight prompt + confirmation).
type osdModel struct {
	svc     *service.Service
	keys    osdKeyMap
	table   table.Model
	osds    []model.OSD
	err     error
	loading bool
	detail  bool

	mode      osdMode
	confirm   components.Confirm
	weight    textinput.Model
	weightErr bool
	filter    tableFilter
	pendingOp service.Operation
	opErr     error

	width  int
	height int
}

func newOSDModel(svc *service.Service) osdModel {
	t := table.New(
		table.WithColumns(osdColumns()),
		table.WithFocused(true),
	)
	t.SetStyles(osdTableStyles())

	w := textinput.New()
	w.Placeholder = "0.00 – 1.00"
	w.CharLimit = 5

	return osdModel{svc: svc, keys: defaultOSDKeys(), table: t, weight: w, filter: newTableFilter(), loading: true}
}

type (
	osdsMsg     struct{ osds []model.OSD }
	osdErrMsg   struct{ err error }
	opResultMsg struct{ err error }
)

// fetch loads the OSD list off the UI goroutine.
func (m osdModel) fetch() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		osds, err := m.svc.OSDs(ctx)
		if err != nil {
			return osdErrMsg{err}
		}
		return osdsMsg{osds}
	}
}

// runOp executes a confirmed operation off the UI goroutine.
func runOp(op service.Operation) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		return opResultMsg{op.Run(ctx)}
	}
}

// capturing reports whether the view currently owns keyboard input (an overlay
// or the "/" filter prompt is open), so the shell must not intercept keys (e.g.
// digits typed to confirm, or text typed into the filter).
func (m osdModel) capturing() bool { return m.mode != osdNormal || m.filter.typing() }

// title names the view in the header and breadcrumb.
func (m osdModel) title() string { return "OSDs" }

// filterPrompt returns the "/" filter line for the layout to render above the
// panel, or "" when the filter is neither open nor applied.
func (m osdModel) filterPrompt() string {
	if !m.filter.visible() {
		return ""
	}
	return m.filter.prompt()
}

// supportsFilter reports that this view offers the "/" filter.
func (m osdModel) supportsFilter() bool { return true }

// actions lists the context-sensitive operations for the action bar. They stay
// shown even while a popup (detail/confirm) is open, so the header doesn't
// visibly collapse — the operator keeps the full context.
func (m osdModel) actions() []Action {
	return []Action{
		act(m.keys.In, false),
		act(m.keys.Out, false),
		act(m.keys.Reweight, false),
		act(m.keys.Destroy, true),
		act(m.keys.Purge, true),
		act(m.keys.Remove, true),
	}
}

func (m *osdModel) setSize(width, height int) {
	m.width, m.height = width, height
	m.table.SetColumns(fitColumns(osdColumns(), width))
	if height > 2 {
		m.table.SetHeight(height - 1) // leave a line for the table header
	}
}

func (m osdModel) Update(msg tea.Msg) (osdModel, tea.Cmd) {
	switch msg := msg.(type) {
	case osdsMsg:
		m.osds = msg.osds
		m.err = nil
		m.opErr = nil // a fresh, successful load clears any stale op error
		m.loading = false
		m.table.SetRows(m.filter.apply(osdRows(m.osds)))
		return m, nil
	case osdErrMsg:
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
		return m, m.fetch() // reflect the change immediately
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Non-key messages (e.g. cursor blink) go to whichever input is active.
	var cmd tea.Cmd
	switch m.mode {
	case osdConfirm:
		m.confirm, cmd = m.confirm.Update(msg)
	case osdWeightPrompt:
		m.weight, cmd = m.weight.Update(msg)
	default:
		m.table, cmd = m.table.Update(msg)
	}
	return m, cmd
}

func (m osdModel) handleKey(msg tea.KeyMsg) (osdModel, tea.Cmd) {
	// While the "/" prompt is open it owns every keystroke.
	if m.filter.typing() {
		cmd, _ := m.filter.handleKey(msg, &m.table, osdRows(m.osds))
		return m, cmd
	}

	switch m.mode {
	case osdConfirm:
		return m.handleConfirmKey(msg)
	case osdWeightPrompt:
		return m.handleWeightKey(msg)
	}

	// Normal browsing: Enter opens the OSD detail view; Esc closes it or clears an
	// applied filter.
	switch {
	case key.Matches(msg, m.keys.Enter):
		if !m.detail {
			if _, ok := m.selectedOSD(); ok {
				m.detail = true
			}
		}
		return m, nil
	case key.Matches(msg, m.keys.Back):
		switch {
		case m.detail:
			m.detail = false
		case m.filter.applied():
			m.filter.clear()
			m.table.SetRows(m.filter.apply(osdRows(m.osds)))
		}
		return m, nil
	}
	if m.detail {
		return m, nil // freeze navigation and operations while detail is open
	}
	return m.dispatch(msg)
}

// dispatch runs a normal-mode action from a direct hotkey. (Enter is handled in
// handleKey — it opens the detail view — so it never reaches here.)
func (m osdModel) dispatch(msg tea.KeyMsg) (osdModel, tea.Cmd) {
	if key.Matches(msg, m.keys.Filter) {
		return m, m.filter.open()
	}
	if o, ok := m.selectedOSD(); ok {
		switch {
		case key.Matches(msg, m.keys.Out):
			return m.startConfirm(m.svc.OSDMarkOut(o.ID))
		case key.Matches(msg, m.keys.In):
			return m.startConfirm(m.svc.OSDMarkIn(o.ID))
		case key.Matches(msg, m.keys.Destroy):
			return m.startConfirm(m.svc.OSDDestroy(o.ID))
		case key.Matches(msg, m.keys.Purge):
			return m.startConfirm(m.svc.OSDPurge(o.ID))
		case key.Matches(msg, m.keys.Remove):
			return m.startConfirm(m.svc.OSDRemove(o.ID))
		case key.Matches(msg, m.keys.Reweight):
			m.mode = osdWeightPrompt
			m.weight.SetValue("")
			m.weightErr = false
			return m, m.weight.Focus()
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m osdModel) handleConfirmKey(msg tea.KeyMsg) (osdModel, tea.Cmd) {
	var cmd tea.Cmd
	m.confirm, cmd = m.confirm.Update(msg)
	switch m.confirm.State() {
	case components.ConfirmAccepted:
		m.mode = osdNormal
		return m, runOp(m.pendingOp)
	case components.ConfirmCancelled:
		m.mode = osdNormal
		return m, nil
	}
	return m, cmd
}

func (m osdModel) handleWeightKey(msg tea.KeyMsg) (osdModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		w, err := strconv.ParseFloat(strings.TrimSpace(m.weight.Value()), 64)
		if err != nil || w < 0 || w > 1 {
			m.weightErr = true
			return m, nil
		}
		o, ok := m.selectedOSD()
		if !ok {
			m.mode = osdNormal
			return m, nil
		}
		m.weight.Blur()
		return m.startConfirm(m.svc.OSDReweight(o.ID, w))
	case "esc":
		m.mode = osdNormal
		m.weight.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.weight, cmd = m.weight.Update(msg)
	return m, cmd
}

// startConfirm opens the y/n confirmation dialog for an operation. Irreversible
// operations keep the red danger styling so destructive actions still stand out.
func (m osdModel) startConfirm(op service.Operation) (osdModel, tea.Cmd) {
	m.pendingOp = op
	m.confirm = components.NewYesNo(op.Title, op.Command, op.Consequence, op.Irreversible)
	m.mode = osdConfirm
	return m, nil
}

func (m osdModel) View(width, height int) string {
	page := m.pageView(width)

	// Popups are composited over the page so the list stays visible behind them.
	switch m.mode {
	case osdConfirm:
		return overlayCenter(page, m.confirm.View(width), width, height)
	case osdWeightPrompt:
		return overlayCenter(page, m.weightView(width), width, height)
	}
	if m.detail {
		if o, ok := m.selectedOSD(); ok {
			return overlayCenter(page, osdDetail(o, width), width, height)
		}
	}
	return page
}

// pageView renders the OSD list (the background behind any popup).
func (m osdModel) pageView(width int) string {
	var prefix string
	if m.opErr != nil {
		prefix = styles.Danger.Render("operation failed: "+m.opErr.Error()) + "\n\n"
	}
	if m.err != nil && m.osds == nil {
		return prefix + errorScreen(m.err)
	}
	if m.err != nil {
		prefix += staleBanner(m.err) + "\n\n"
	}
	switch {
	case m.loading && m.osds == nil:
		return prefix + styles.Faint.Render("Loading OSDs…")
	case len(m.osds) == 0:
		return prefix + styles.Faint.Render("No OSDs reported.")
	}
	return prefix + m.table.View()
}

func (m osdModel) weightView(width int) string {
	o, _ := m.selectedOSD()

	var b strings.Builder
	b.WriteString(styles.PanelTitle.Render(fmt.Sprintf("Reweight OSD.%d", o.ID)) + "\n\n")
	b.WriteString("New weight (0.00 – 1.00):\n")
	b.WriteString(m.weight.View())
	if m.weightErr {
		b.WriteString("\n" + styles.Danger.Render("enter a number between 0 and 1"))
	}
	b.WriteString("\n\n" + styles.Faint.Render("[enter] continue   [esc] cancel"))

	w := width - 4
	if w > 48 {
		w = 48
	}
	if w < 30 {
		w = 30
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.AccentColor()).
		Padding(0, 1).
		Width(w).
		Render(b.String())
}

// selectedOSD returns the OSD under the table cursor. When a filter is applied
// the displayed rows are a subset, so the cursor is mapped back to the index in
// the full m.osds slice via the filter.
func (m osdModel) selectedOSD() (model.OSD, bool) {
	if i, ok := m.filter.source(m.table.Cursor()); ok && i < len(m.osds) {
		return m.osds[i], true
	}
	return model.OSD{}, false
}

func osdColumns() []table.Column {
	return []table.Column{
		{Title: "ID", Width: 4},
		{Title: "HOST", Width: 12},
		{Title: "CLASS", Width: 6},
		{Title: "STATUS", Width: 8},
		{Title: "REWEIGHT", Width: 9},
		{Title: "%USE", Width: 6},
		{Title: "PGS", Width: 5},
		{Title: "SIZE", Width: 10},
	}
}

func osdRows(osds []model.OSD) []table.Row {
	rows := make([]table.Row, 0, len(osds))
	for _, o := range osds {
		rows = append(rows, table.Row{
			strconv.Itoa(o.ID),
			o.Host,
			o.DeviceClass,
			o.Status(),
			fmt.Sprintf("%.2f", o.Reweight),
			format.Percent(o.UsedRatio),
			strconv.Itoa(o.PGs),
			format.Bytes(o.SizeBytes),
		})
	}
	return rows
}

func osdTableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.Bold(true).Foreground(styles.AccentColor()).BorderBottom(true)
	s.Selected = s.Selected.Bold(true).Foreground(styles.SelectedFg()).Background(styles.SelectedBg())
	return s
}

// osdDetail renders the metadata panel for a single OSD.
func osdDetail(o model.OSD, width int) string {
	body := fmt.Sprintf(
		"host        %s\nclass       %s\nstatus      %s\nreweight    %.2f\ncrush wt    %.4f\nutilization %s\npgs         %d\nsize        %s\nused        %s",
		o.Host,
		o.DeviceClass,
		o.Status(),
		o.Reweight,
		o.CrushWeight,
		format.Percent(o.UsedRatio),
		o.PGs,
		format.Bytes(o.SizeBytes),
		format.Bytes(o.UsedBytes),
	)
	panelWidth := width - 2
	if panelWidth > 48 {
		panelWidth = 48
	}
	return components.Panel(fmt.Sprintf("OSD.%d", o.ID), body, panelWidth, 9)
}
