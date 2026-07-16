package ui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cinpol/siphon/internal/model"
	"github.com/cinpol/siphon/internal/service"
	"github.com/cinpol/siphon/internal/ui/components"
	"github.com/cinpol/siphon/internal/ui/styles"
)

// svcLevel is which level of the service view is shown.
type svcLevel int

const (
	svcLevelServices svcLevel = iota
	svcLevelDaemons
)

type serviceKeyMap struct {
	Nav     key.Binding
	Enter   key.Binding
	Back    key.Binding
	Filter  key.Binding
	Restart key.Binding
	Start   key.Binding
	Stop    key.Binding
}

func defaultServiceKeys() serviceKeyMap {
	return serviceKeyMap{
		Nav:     key.NewBinding(key.WithKeys("up", "down", "j", "k"), key.WithHelp("↑/↓", "navigate")),
		Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "daemons")),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Restart: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "restart")),
		Start:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start")),
		Stop:    key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "stop")),
	}
}

// serviceModel is the two-level service view: a services list that drills down
// into the daemons of the selected service.
type serviceModel struct {
	svc  *service.Service
	keys serviceKeyMap

	level    svcLevel
	svcTable table.Model
	dmnTable table.Model
	services []model.Service
	daemons  []model.Daemon
	current  string // service name when drilled into daemons

	// orchestrator is the cluster's deployment type, kept in sync from the
	// dashboard. On a non-cephadm cluster (Rook/manual) `orch` commands don't
	// exist, so the view shows an explanatory state instead of failing. Empty
	// until the first dashboard load; treated as cephadm (attempt the call).
	orchestrator model.Orchestrator

	err     error
	loading bool

	confirming bool
	confirm    components.Confirm
	filter     tableFilter
	pendingOp  service.Operation
	opErr      error

	width  int
	height int
}

func newServiceModel(svc *service.Service) serviceModel {
	st := table.New(table.WithColumns(serviceColumns()), table.WithFocused(true))
	st.SetStyles(osdTableStyles())
	dt := table.New(table.WithColumns(daemonColumns()), table.WithFocused(true))
	dt.SetStyles(osdTableStyles())
	return serviceModel{svc: svc, keys: defaultServiceKeys(), svcTable: st, dmnTable: dt, filter: newTableFilter(), loading: true}
}

type (
	servicesMsg struct{ services []model.Service }
	daemonsMsg  struct {
		service string
		daemons []model.Daemon
	}
	serviceErrMsg struct{ err error }
)

func (m serviceModel) fetch() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		svcs, err := m.svc.Services(ctx)
		if err != nil {
			return serviceErrMsg{err}
		}
		return servicesMsg{svcs}
	}
}

func (m serviceModel) fetchDaemons(name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		dmns, err := m.svc.Daemons(ctx, name)
		if err != nil {
			return serviceErrMsg{err}
		}
		return daemonsMsg{service: name, daemons: dmns}
	}
}

// refresh reloads whichever level is active.
func (m serviceModel) refresh() tea.Cmd {
	if m.cephadmUnavailable() {
		return nil // no orchestrator to query; the view explains this instead
	}
	if m.level == svcLevelDaemons {
		return m.fetchDaemons(m.current)
	}
	return m.fetch()
}

// cephadmUnavailable reports whether the view should show its non-cephadm
// explanation instead of orchestrator data. True only once a non-cephadm cluster
// is positively detected; an unknown orchestrator (before the first dashboard
// load) is treated as cephadm so the call is still attempted.
func (m serviceModel) cephadmUnavailable() bool {
	return m.orchestrator == model.OrchestratorNone
}

func (m serviceModel) capturing() bool { return m.confirming || m.filter.typing() }

func (m serviceModel) title() string {
	if m.level == svcLevelDaemons {
		return "Services · daemons · " + m.current
	}
	return "Services"
}

func (m serviceModel) filterPrompt() string {
	if !m.filter.visible() {
		return ""
	}
	return m.filter.prompt()
}

func (m serviceModel) supportsFilter() bool { return !m.cephadmUnavailable() }

func (m serviceModel) actions() []Action {
	if m.cephadmUnavailable() {
		return nil // nothing to act on without a cephadm orchestrator
	}
	if m.level == svcLevelDaemons {
		return []Action{
			act(m.keys.Restart, false),
			act(m.keys.Start, false),
			act(m.keys.Stop, true),
			act(m.keys.Back, false),
		}
	}
	return []Action{
		act(m.keys.Enter, false),
		act(m.keys.Restart, false),
	}
}

// activeRows returns the rows of the level currently on screen, for the filter.
func (m serviceModel) activeRows() []table.Row {
	if m.level == svcLevelDaemons {
		return daemonRows(m.daemons)
	}
	return serviceRows(m.services)
}

// reapplyFilter refreshes the active level's table rows through the filter (used
// after clearing the filter so the full list returns).
func (m *serviceModel) reapplyFilter() {
	if m.level == svcLevelDaemons {
		m.dmnTable.SetRows(m.filter.apply(daemonRows(m.daemons)))
	} else {
		m.svcTable.SetRows(m.filter.apply(serviceRows(m.services)))
	}
}

func (m *serviceModel) setSize(width, height int) {
	m.width, m.height = width, height
	m.svcTable.SetColumns(fitColumns(serviceColumns(), width))
	m.dmnTable.SetColumns(fitColumns(daemonColumns(), width))
	if height > 3 {
		m.svcTable.SetHeight(height - 1)
		m.dmnTable.SetHeight(height - 1)
	}
}

func (m serviceModel) Update(msg tea.Msg) (serviceModel, tea.Cmd) {
	switch msg := msg.(type) {
	case servicesMsg:
		m.err = nil
		m.opErr = nil // a fresh, successful load clears any stale op error
		m.loading = false
		m.services = msg.services
		m.svcTable.SetRows(m.filter.apply(serviceRows(m.services)))
		return m, nil
	case daemonsMsg:
		m.err = nil
		m.opErr = nil
		m.loading = false
		m.daemons = msg.daemons
		m.dmnTable.SetRows(m.filter.apply(daemonRows(m.daemons)))
		return m, nil
	case serviceErrMsg:
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
		return m, m.refresh()
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	if m.confirming {
		m.confirm, cmd = m.confirm.Update(msg)
	} else if m.level == svcLevelDaemons {
		m.dmnTable, cmd = m.dmnTable.Update(msg)
	} else {
		m.svcTable, cmd = m.svcTable.Update(msg)
	}
	return m, cmd
}

func (m serviceModel) handleKey(msg tea.KeyMsg) (serviceModel, tea.Cmd) {
	if m.filter.typing() {
		t := &m.svcTable
		if m.level == svcLevelDaemons {
			t = &m.dmnTable
		}
		cmd, _ := m.filter.handleKey(msg, t, m.activeRows())
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
	if key.Matches(msg, m.keys.Filter) {
		return m, m.filter.open()
	}
	// Esc clears an applied filter before it acts as "back".
	if key.Matches(msg, m.keys.Back) && m.filter.applied() {
		m.filter.clear()
		m.reapplyFilter()
		return m, nil
	}

	// Enter (at the services level) drills into the selected service's daemons;
	// every other key dispatches to the active level's handler.
	return m.dispatch(msg)
}

// dispatch routes a normal-mode key to the active level's handler, whether from
// a hotkey or a replayed menu selection.
func (m serviceModel) dispatch(msg tea.KeyMsg) (serviceModel, tea.Cmd) {
	if m.level == svcLevelDaemons {
		return m.handleDaemonKey(msg)
	}
	return m.handleServiceKey(msg)
}

func (m serviceModel) handleServiceKey(msg tea.KeyMsg) (serviceModel, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Enter):
		if s, ok := m.selectedService(); ok {
			m.current = s.Name
			m.level = svcLevelDaemons
			m.daemons = nil
			m.loading = true
			m.filter.clear() // the daemon list starts unfiltered
			return m, m.fetchDaemons(s.Name)
		}
		return m, nil
	case key.Matches(msg, m.keys.Restart):
		if s, ok := m.selectedService(); ok {
			return m.confirmOp(m.svc.ServiceRestart(s.Name))
		}
	}
	var cmd tea.Cmd
	m.svcTable, cmd = m.svcTable.Update(msg)
	return m, cmd
}

func (m serviceModel) handleDaemonKey(msg tea.KeyMsg) (serviceModel, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.level = svcLevelServices
		m.filter.clear() // return to the services list unfiltered
		m.reapplyFilter()
		return m, nil
	case key.Matches(msg, m.keys.Restart):
		if d, ok := m.selectedDaemon(); ok {
			return m.confirmOp(m.svc.DaemonRestart(d.Name))
		}
	case key.Matches(msg, m.keys.Start):
		if d, ok := m.selectedDaemon(); ok {
			return m.confirmOp(m.svc.DaemonStart(d.Name))
		}
	case key.Matches(msg, m.keys.Stop):
		if d, ok := m.selectedDaemon(); ok {
			return m.confirmOp(m.svc.DaemonStop(d.Name))
		}
	}
	var cmd tea.Cmd
	m.dmnTable, cmd = m.dmnTable.Update(msg)
	return m, cmd
}

// confirmOp opens a y/N confirmation for an orchestrator operation (all
// service/daemon actions are reversible, per the project decision).
func (m serviceModel) confirmOp(op service.Operation) (serviceModel, tea.Cmd) {
	m.pendingOp = op
	m.confirm = components.NewYesNo(op.Title, op.Command, op.Consequence, false)
	m.confirming = true
	return m, nil
}

func (m serviceModel) View(width, height int) string {
	page := m.pageView(width)
	if m.confirming {
		return overlayCenter(page, m.confirm.View(width), width, height)
	}
	return page
}

func (m serviceModel) pageView(width int) string {
	// On a non-cephadm cluster there is no orchestrator to query, so explain that
	// rather than showing a failed `orch` call.
	if m.cephadmUnavailable() {
		return servicesUnavailableBody()
	}

	prefix := ""
	if m.opErr != nil {
		prefix = styles.Danger.Render("operation failed: "+m.opErr.Error()) + "\n\n"
	}
	haveData := len(m.services) > 0 || len(m.daemons) > 0
	if m.err != nil && !haveData {
		return prefix + styles.Danger.Render("Error: ") + m.err.Error() +
			styles.Faint.Render("\n\n(is the cephadm orchestrator enabled?)")
	}
	if m.err != nil {
		prefix += staleBanner(m.err) + "\n\n"
	}

	if m.level == svcLevelDaemons {
		if m.loading && m.daemons == nil {
			return prefix + styles.Faint.Render("Loading daemons…")
		}
		return prefix + m.dmnTable.View()
	}

	if m.loading && m.services == nil {
		return prefix + styles.Faint.Render("Loading services…")
	}
	return prefix + m.svcTable.View()
}

// servicesUnavailableBody explains why the Services view has no data on a cluster
// without a cephadm orchestrator (Rook or a manual deployment). The rest of
// Siphon works normally; only cephadm-based service management is unavailable.
func servicesUnavailableBody() string {
	title := styles.Label.Render("No cephadm orchestrator detected")
	body := styles.Faint.Render(
		"This cluster isn't managed by cephadm, so service management isn't available here.\n" +
			"Rook and manually-deployed clusters manage their Ceph daemons outside Ceph\n" +
			"(for Rook, via Kubernetes — e.g. `kubectl -n rook-ceph`).\n\n" +
			"Dashboard, OSDs, Pools, CRUSH, Flags and PGs all work normally.")
	return title + "\n\n" + body
}

func (m serviceModel) selectedService() (model.Service, bool) {
	if i, ok := m.filter.source(m.svcTable.Cursor()); ok && i < len(m.services) {
		return m.services[i], true
	}
	return model.Service{}, false
}

func (m serviceModel) selectedDaemon() (model.Daemon, bool) {
	if i, ok := m.filter.source(m.dmnTable.Cursor()); ok && i < len(m.daemons) {
		return m.daemons[i], true
	}
	return model.Daemon{}, false
}

func serviceColumns() []table.Column {
	return []table.Column{
		{Title: "SERVICE", Width: 16},
		{Title: "RUNNING", Width: 8},
		{Title: "PLACEMENT", Width: 20},
		{Title: "STATUS", Width: 10},
	}
}

func serviceRows(services []model.Service) []table.Row {
	rows := make([]table.Row, 0, len(services))
	for _, s := range services {
		status := "running"
		if !s.Healthy() {
			status = "degraded"
		}
		rows = append(rows, table.Row{
			s.Name,
			fmt.Sprintf("%d/%d", s.Running, s.Size),
			s.Placement,
			status,
		})
	}
	return rows
}

func daemonColumns() []table.Column {
	return []table.Column{
		{Title: "NAME", Width: 28},
		{Title: "HOST", Width: 12},
		{Title: "STATUS", Width: 9},
		{Title: "VERSION", Width: 9},
	}
}

func daemonRows(daemons []model.Daemon) []table.Row {
	rows := make([]table.Row, 0, len(daemons))
	for _, d := range daemons {
		rows = append(rows, table.Row{d.Name, d.Host, d.Status, d.Version})
	}
	return rows
}
