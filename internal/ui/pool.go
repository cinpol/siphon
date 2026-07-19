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

// poolMode is the pool view's interaction state.
type poolMode int

const (
	poolNormal poolMode = iota
	poolForm
	poolConfirm
)

type poolFormKind int

const (
	poolFormCreate poolFormKind = iota
	poolFormEdit
)

// poolKeyMap holds the pool view's bindings.
type poolKeyMap struct {
	Nav    key.Binding
	Enter  key.Binding
	Back   key.Binding
	Filter key.Binding
	Create key.Binding
	Edit   key.Binding
	Delete key.Binding
}

func defaultPoolKeys() poolKeyMap {
	return poolKeyMap{
		Nav:    key.NewBinding(key.WithKeys("up", "down", "j", "k"), key.WithHelp("↑/↓", "navigate")),
		Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
		Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Filter: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Create: key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "create")),
		Edit:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Delete: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	}
}

// poolModel is the stateful pool view: a navigable table plus the create/edit
// forms and the delete flow, all guarded by the type-to-confirm dialog.
type poolModel struct {
	svc     *service.Service
	keys    poolKeyMap
	table   table.Model
	pools   []model.Pool
	err     error
	loading bool
	detail  bool

	mode      poolMode
	formKind  poolFormKind
	form      components.Form
	confirm   components.Confirm
	filter    tableFilter
	sort      tableSort[model.Pool]
	pendingOp service.Operation
	editing   model.Pool
	opErr     error
	notice    string

	width  int
	height int
}

func newPoolModel(svc *service.Service) poolModel {
	t := table.New(
		table.WithColumns(poolColumns()),
		table.WithFocused(true),
	)
	t.SetStyles(osdTableStyles())
	return poolModel{
		svc:     svc,
		keys:    defaultPoolKeys(),
		table:   t,
		filter:  newTableFilter(),
		sort:    newPoolSort(),
		loading: true,
	}
}

// newPoolSort defines the Pools table's sort columns. Each key is Shift+<letter>
// (k9s-style), matching the column's initial. Replica size isn't a sort column
// (it rarely drives browsing), which frees ⇧S for STORED.
func newPoolSort() tableSort[model.Pool] {
	return newTableSort(
		sortKey[model.Pool]{"NAME", "N", func(a, b model.Pool) bool { return a.Name < b.Name }},
		sortKey[model.Pool]{"PG_NUM", "P", func(a, b model.Pool) bool { return a.PGNum < b.PGNum }},
		sortKey[model.Pool]{"%USED", "U", func(a, b model.Pool) bool { return a.UsedRatio < b.UsedRatio }},
		sortKey[model.Pool]{"STORED", "S", func(a, b model.Pool) bool { return a.StoredBytes < b.StoredBytes }},
		sortKey[model.Pool]{"OBJECTS", "O", func(a, b model.Pool) bool { return a.Objects < b.Objects }},
	)
}

// applyColumns sets the table columns for the current width, adding the active
// sort column's ↑/↓ arrow. Called on resize and whenever the sort changes.
func (m *poolModel) applyColumns() {
	m.table.SetColumns(fitColumns(m.sort.decorate(poolColumns()), m.width))
}

// refreshRows re-sorts the pool slice and rebuilds the (filtered) table rows.
// Sorting in place keeps the filter's row→source map and the selection valid.
func (m *poolModel) refreshRows() {
	m.sort.apply(m.pools)
	m.table.SetRows(m.filter.apply(poolRows(m.pools)))
}

type (
	poolsMsg   struct{ pools []model.Pool }
	poolErrMsg struct{ err error }
)

func (m poolModel) fetch() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		pools, err := m.svc.Pools(ctx)
		if err != nil {
			return poolErrMsg{err}
		}
		return poolsMsg{pools}
	}
}

func (m poolModel) capturing() bool { return m.mode != poolNormal || m.filter.typing() }

func (m poolModel) title() string { return "Pools" }

func (m poolModel) filterPrompt() string {
	if !m.filter.visible() {
		return ""
	}
	return m.filter.prompt()
}

func (m poolModel) supportsFilter() bool { return true }

func (m poolModel) actions() []Action {
	return []Action{
		act(m.keys.Create, false),
		act(m.keys.Edit, false),
		act(m.keys.Delete, true),
		m.sort.hint(),
	}
}

func (m *poolModel) setSize(width, height int) {
	m.width, m.height = width, height
	m.applyColumns()
	if height > 2 {
		m.table.SetHeight(height - 1)
	}
}

func (m poolModel) Update(msg tea.Msg) (poolModel, tea.Cmd) {
	switch msg := msg.(type) {
	case poolsMsg:
		m.pools = msg.pools
		m.err = nil
		m.opErr = nil // a fresh, successful load clears any stale op error
		m.loading = false
		m.refreshRows()
		return m, nil
	case poolErrMsg:
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

	var cmd tea.Cmd
	switch m.mode {
	case poolForm:
		m.form, cmd = m.form.Update(msg)
	case poolConfirm:
		m.confirm, cmd = m.confirm.Update(msg)
	default:
		m.table, cmd = m.table.Update(msg)
	}
	return m, cmd
}

func (m poolModel) handleKey(msg tea.KeyMsg) (poolModel, tea.Cmd) {
	if m.filter.typing() {
		cmd, _ := m.filter.handleKey(msg, &m.table, poolRows(m.pools))
		return m, cmd
	}

	switch m.mode {
	case poolForm:
		return m.handleFormKey(msg)
	case poolConfirm:
		return m.handleConfirmKey(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Enter):
		if !m.detail {
			if _, ok := m.selectedPool(); ok {
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
			m.refreshRows()
		}
		return m, nil
	}
	if m.detail {
		return m, nil
	}
	return m.dispatch(msg)
}

// dispatch runs a normal-mode action from a direct hotkey. (Enter is handled in
// handleKey — it opens the detail view — so it never reaches here.)
func (m poolModel) dispatch(msg tea.KeyMsg) (poolModel, tea.Cmd) {
	// Shift+<column> re-sorts the table (k9s-style). Handled before the other
	// hotkeys, though the sort keys (uppercase) don't overlap with them.
	if m.sort.handleKey(msg) {
		m.applyColumns()
		m.refreshRows()
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Filter):
		return m, m.filter.open()
	case key.Matches(msg, m.keys.Create):
		m.formKind = poolFormCreate
		m.form = newCreatePoolForm()
		m.mode = poolForm
		m.notice = ""
		return m, textinput.Blink
	case key.Matches(msg, m.keys.Edit):
		if p, ok := m.selectedPool(); ok {
			m.formKind = poolFormEdit
			m.editing = p
			m.form = newEditPoolForm(p)
			m.mode = poolForm
			m.notice = ""
			return m, textinput.Blink
		}
	case key.Matches(msg, m.keys.Delete):
		if p, ok := m.selectedPool(); ok {
			return m.startConfirm(m.svc.PoolDelete(p.Name))
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m poolModel) handleFormKey(msg tea.KeyMsg) (poolModel, tea.Cmd) {
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	switch m.form.State() {
	case components.FormSubmitted:
		return m.submitForm()
	case components.FormCancelled:
		m.mode = poolNormal
		return m, nil
	}
	return m, cmd
}

func (m poolModel) submitForm() (poolModel, tea.Cmd) {
	if m.formKind == poolFormCreate {
		name := m.form.Value(0)
		if name == "" {
			m.form = m.form.Reactivate()
			m.notice = "pool name is required"
			return m, nil
		}
		spec := service.PoolCreateSpec{
			Name:        name,
			Size:        atoiDefault(m.form.Value(1), 0),
			MinSize:     atoiDefault(m.form.Value(2), 0),
			PGNum:       atoiDefault(m.form.Value(3), 0),
			Autoscale:   m.form.Value(4),
			CrushRule:   m.form.Value(5),
			Application: m.form.Value(6),
		}
		return m.startConfirm(m.svc.PoolCreate(spec))
	}

	spec := service.PoolEditSpec{
		Size:        atoiDefault(m.form.Value(0), 0),
		MinSize:     atoiDefault(m.form.Value(1), 0),
		PGNum:       atoiDefault(m.form.Value(2), 0),
		Autoscale:   m.form.Value(3),
		CrushRule:   m.form.Value(4),
		Application: m.form.Value(5),
	}
	op := m.svc.PoolEdit(m.editing, spec)
	if op.Empty() {
		m.mode = poolNormal
		m.notice = "no changes to apply"
		return m, nil
	}
	return m.startConfirm(op)
}

func (m poolModel) handleConfirmKey(msg tea.KeyMsg) (poolModel, tea.Cmd) {
	var cmd tea.Cmd
	m.confirm, cmd = m.confirm.Update(msg)
	switch m.confirm.State() {
	case components.ConfirmAccepted:
		m.mode = poolNormal
		return m, runOp(m.pendingOp)
	case components.ConfirmCancelled:
		m.mode = poolNormal
		return m, nil
	}
	return m, cmd
}

func (m poolModel) startConfirm(op service.Operation) (poolModel, tea.Cmd) {
	m.pendingOp = op
	m.confirm = components.NewYesNo(op.Title, op.Command, op.Consequence, op.Irreversible)
	m.mode = poolConfirm
	m.notice = ""
	return m, nil
}

func (m poolModel) View(width, height int) string {
	page := m.pageView(width)
	switch m.mode {
	case poolForm:
		return overlayCenter(page, m.form.View(width), width, height)
	case poolConfirm:
		return overlayCenter(page, m.confirm.View(width), width, height)
	}
	if m.detail {
		if p, ok := m.selectedPool(); ok {
			return overlayCenter(page, poolDetail(p, width), width, height)
		}
	}
	return page
}

func (m poolModel) pageView(width int) string {
	prefix := ""
	switch {
	case m.opErr != nil:
		prefix = styles.Danger.Render("operation failed: "+m.opErr.Error()) + "\n\n"
	case m.notice != "":
		prefix = styles.Faint.Render(m.notice) + "\n\n"
	}

	if m.err != nil && m.pools == nil {
		return prefix + errorScreen(m.err)
	}
	if m.err != nil {
		prefix += staleBanner(m.err) + "\n\n"
	}

	switch {
	case m.loading && m.pools == nil:
		return prefix + styles.Faint.Render("Loading pools…")
	case len(m.pools) == 0:
		return prefix + styles.Faint.Render("No pools reported.")
	}
	return prefix + m.table.View()
}

func (m poolModel) selectedPool() (model.Pool, bool) {
	if i, ok := m.filter.source(m.table.Cursor()); ok && i < len(m.pools) {
		return m.pools[i], true
	}
	return model.Pool{}, false
}

// poolAutoscaleModes are the autoscaler choices, offered identically in create
// and edit so the two forms stay consistent.
var poolAutoscaleModes = []string{"on", "off", "warn"}

// Create and Edit share the same property fields (Size, Min size, PG num,
// Autoscale, CRUSH rule, Application) in the same order; Create additionally
// asks for the pool Name.

func newCreatePoolForm() components.Form {
	return components.NewForm("Create pool (replicated)",
		components.TextField("Name", "mypool", ""),
		components.TextField("Size", "3", "3"),
		components.TextField("Min size", "2", "2"),
		components.TextField("PG num", "autoscale-managed", ""),
		components.ChoiceField("Autoscale", poolAutoscaleModes, 0),
		components.TextField("CRUSH rule", "replicated_rule", "replicated_rule"),
		components.TextField("Application", "rbd", "rbd"),
	)
}

func newEditPoolForm(p model.Pool) components.Form {
	app := ""
	if len(p.Applications) > 0 {
		app = p.Applications[0]
	}
	return components.NewForm(fmt.Sprintf("Edit pool: %s", p.Name),
		components.TextField("Size", "", strconv.Itoa(p.Size)),
		components.TextField("Min size", "", strconv.Itoa(p.MinSize)),
		components.TextField("PG num", "", strconv.Itoa(p.PGNum)),
		components.ChoiceField("Autoscale", poolAutoscaleModes, indexOf(poolAutoscaleModes, p.AutoscaleMode)),
		components.TextField("CRUSH rule", "", p.CrushRule),
		components.TextField("Application", "rbd", app),
	)
}

func poolColumns() []table.Column {
	return []table.Column{
		{Title: "NAME", Width: 16},
		{Title: "TYPE", Width: 10},
		{Title: "SIZE", Width: 5},
		{Title: "MIN_SIZE", Width: 8},
		{Title: "PG_NUM", Width: 7},
		{Title: "PGP_NUM", Width: 8},
		{Title: "AUTOSCALE", Width: 9},
		{Title: "%USED", Width: 6},
		{Title: "STORED", Width: 9},
		{Title: "OBJECTS", Width: 8},
		{Title: "APPS", Width: 10},
	}
}

func poolRows(pools []model.Pool) []table.Row {
	rows := make([]table.Row, 0, len(pools))
	for _, p := range pools {
		rows = append(rows, table.Row{
			p.Name,
			p.Type,
			strconv.Itoa(p.Size),
			strconv.Itoa(p.MinSize),
			strconv.Itoa(p.PGNum),
			strconv.Itoa(p.PGPNum),
			p.AutoscaleMode,
			format.Percent(p.UsedRatio),
			format.Bytes(p.StoredBytes),
			format.Count(p.Objects),
			strings.Join(p.Applications, ","),
		})
	}
	return rows
}

func poolDetail(p model.Pool, width int) string {
	body := fmt.Sprintf(
		"type        %s\nsize/min    %d / %d\npg_num      %d\npgp_num     %d\ncrush rule  %s\nautoscale   %s\napps        %s\nstored      %s\nobjects     %s\n%%used       %s",
		p.Type,
		p.Size, p.MinSize,
		p.PGNum,
		p.PGPNum,
		p.CrushRule,
		p.AutoscaleMode,
		strings.Join(p.Applications, ", "),
		format.Bytes(p.StoredBytes),
		format.Count(p.Objects),
		format.Percent(p.UsedRatio),
	)
	panelWidth := width - 2
	if panelWidth > 52 {
		panelWidth = 52
	}
	return components.Panel(fmt.Sprintf("Pool: %s", p.Name), body, panelWidth, 10)
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}

func indexOf(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i
		}
	}
	return 0
}
