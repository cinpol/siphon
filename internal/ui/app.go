// Package ui contains the Bubble Tea application.
//
// Bubble Tea follows the Elm architecture: an immutable Model, an Update
// function that folds messages into a new Model, and a View that renders the
// Model to a string. All cluster access happens off the UI goroutine inside
// tea.Cmd functions, which return messages the Update loop folds in — the view
// itself never blocks on I/O.
//
// This Model is the k9s-style application shell. The whole screen is arranged by
// a single layout manager (see layout.go): a full-width separator, a columnar
// header (cluster info, navigation, dynamic actions), and a full-size titled
// workspace panel that hosts the active view. Views are switched with number
// keys or the `:` command prompt.
package ui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cinpol/siphon/internal/model"
	"github.com/cinpol/siphon/internal/service"
	"github.com/cinpol/siphon/internal/ui/styles"
	"github.com/cinpol/siphon/internal/ui/views"
)

// fetchTimeout bounds each cluster poll so a slow or unreachable cluster never
// hangs the refresh loop.
const fetchTimeout = 5 * time.Second

// viewID identifies the active content view.
type viewID int

const (
	viewDashboard viewID = iota
	viewOSD
	viewPool
	viewCrush
	viewFlags
	viewServices
	viewPGs
)

// Model is the root Bubble Tea model (the application shell).
type Model struct {
	svc        *service.Service
	interval   time.Duration
	clientName string
	poolRows   int // how many pools the dashboard lists (from config)
	keys       keyMap

	view viewID

	// Dashboard view state.
	dash        *model.Dashboard
	dashErr     error
	dashLoading bool
	lastSync    time.Time

	// Health-detail overlay: the dashboard's `ceph health detail` section can far
	// exceed the panel, so Enter opens this scrollable viewport over the dashboard
	// (Esc closes). See health_detail.go.
	healthDetail bool
	healthVP     viewport.Model
	// dashBG caches the rendered dashboard body while the overlay is open. The
	// grid behind the overlay is static between refreshes, so caching it means a
	// burst of scroll keys re-composites the (changing) overlay over a ready-made
	// background instead of rebuilding the whole grid each frame. Refreshed on
	// open, data refresh and resize (see loadHealthDetail); cleared on close.
	dashBG string

	// Resource views (self-contained sub-models).
	osd      osdModel
	pool     poolModel
	crush    crushModel
	flags    flagModel
	services serviceModel
	pgs      pgModel

	// Command prompt (`:`).
	cmd       textinput.Model
	cmdActive bool

	width  int
	height int
}

// New constructs the root model. clientName is shown in the header so the
// operator can see whether they are looking at a live cluster or the mock.
// poolRows is how many pools the dashboard lists and problemFlags is the PG
// "problems only" flag list — both resolved from config.
func New(svc *service.Service, interval time.Duration, poolRows int, problemFlags []string, clientName string) Model {
	ci := textinput.New()
	ci.Prompt = ":"
	ci.CharLimit = 32

	return Model{
		svc:         svc,
		interval:    interval,
		clientName:  clientName,
		poolRows:    poolRows,
		keys:        defaultKeys(),
		osd:         newOSDModel(svc),
		pool:        newPoolModel(svc),
		crush:       newCrushModel(svc),
		flags:       newFlagModel(svc),
		services:    newServiceModel(svc),
		pgs:         newPGModel(svc, problemFlags),
		cmd:         ci,
		healthVP:    viewport.New(0, 0),
		dashLoading: true,
	}
}

// Messages folded in by Update.
type (
	dashMsg struct{ dash *model.Dashboard }
	errMsg  struct{ err error }
	tickMsg time.Time
)

// Init kicks off the first dashboard fetch and starts the refresh ticker.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetchDash(), m.tick())
}

func (m Model) fetchDash() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		d, err := m.svc.Dashboard(ctx)
		if err != nil {
			return errMsg{err}
		}
		return dashMsg{d}
	}
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(m.interval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// refreshCurrent returns the command that reloads whichever view is active.
func (m *Model) refreshCurrent() tea.Cmd {
	switch m.view {
	case viewOSD:
		m.osd.loading = true
		return m.osd.fetch()
	case viewPool:
		m.pool.loading = true
		return m.pool.fetch()
	case viewCrush:
		m.crush.loading = true
		return m.crush.fetch()
	case viewFlags:
		m.flags.loading = true
		return m.flags.fetch()
	case viewServices:
		m.services.loading = true
		return m.services.refresh()
	case viewPGs:
		m.pgs.loading = true
		return m.pgs.fetch()
	default:
		m.dashLoading = true
		return m.fetchDash()
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.cmd.Width = msg.Width - 2
		for _, v := range m.allViews() {
			v.setSize(m.contentWidth(), m.contentHeight())
		}
		if m.healthDetail {
			m.loadHealthDetail()
		}

	case tea.KeyMsg:
		return m.handleKey(msg)

	case dashMsg:
		m.dash = msg.dash
		m.dashErr = nil
		m.dashLoading = false
		m.lastSync = time.Now()
		// Keep the Services view's orchestrator awareness in sync, so on a
		// non-cephadm cluster it shows its explanation instead of failing `orch`.
		m.services.orchestrator = msg.dash.Orchestrator
		// Keep an open Health-detail overlay in step with fresh data: reload its
		// content (the viewport preserves the scroll offset), and close it if the
		// cluster has recovered and there is nothing left to show.
		if m.healthDetail {
			if m.healthDetailHasContent() {
				m.loadHealthDetail()
			} else {
				m.closeHealthDetail()
			}
		}

	case errMsg:
		m.dashErr = msg.err
		m.dashLoading = false
		// Refresh the cached background so a stale banner shows behind an open
		// overlay instead of the last cache without it.
		if m.healthDetail {
			m.dashBG = m.renderDashboard()
		}

	case osdsMsg, osdErrMsg:
		var cmd tea.Cmd
		m.osd, cmd = m.osd.Update(msg)
		return m, cmd

	case poolsMsg, poolErrMsg:
		var cmd tea.Cmd
		m.pool, cmd = m.pool.Update(msg)
		return m, cmd

	case crushMsg, crushRulesMsg, crushErrMsg:
		var cmd tea.Cmd
		m.crush, cmd = m.crush.Update(msg)
		return m, cmd

	case flagsMsg, flagErrMsg:
		var cmd tea.Cmd
		m.flags, cmd = m.flags.Update(msg)
		return m, cmd

	case servicesMsg, daemonsMsg, nodeDaemonsMsg, serviceErrMsg:
		var cmd tea.Cmd
		m.services, cmd = m.services.Update(msg)
		return m, cmd

	case pgsMsg, pgErrMsg:
		var cmd tea.Cmd
		m.pgs, cmd = m.pgs.Update(msg)
		return m, cmd

	case opResultMsg:
		// Operations originate from whichever view is active; route the result
		// back to it.
		return m.delegateMsg(msg)

	case tickMsg:
		// Always refresh the dashboard so the header (health/version/updated)
		// stays live even while another view is active; also refresh whatever
		// view is on screen.
		cmds := []tea.Cmd{m.tick(), m.refreshCurrent()}
		if m.view != viewDashboard {
			cmds = append(cmds, m.fetchDash())
		}
		return m, tea.Batch(cmds...)
	}

	return m, nil
}

// handleKey routes key presses: the command prompt first (when open), then
// global bindings, then the active view.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.cmdActive {
		switch msg.String() {
		case "enter":
			target := strings.TrimSpace(m.cmd.Value())
			m.cmdActive = false
			m.cmd.Blur()
			m.cmd.SetValue("")
			return m.runCommand(target)
		case "esc":
			m.cmdActive = false
			m.cmd.Blur()
			m.cmd.SetValue("")
			return m, nil
		}
		var cmd tea.Cmd
		m.cmd, cmd = m.cmd.Update(msg)
		return m, cmd
	}

	// When the active view owns input (an overlay is open), route keys straight
	// to it — otherwise digits typed to confirm would trigger view-switch
	// hotkeys. ctrl+c still quits as a safety escape hatch.
	if m.viewCapturing() {
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		return m.delegateKey(msg)
	}

	// The dashboard is not a resourceView, so its one stateful interaction — the
	// scrollable Health-detail overlay — is handled here. While the overlay is
	// open it captures input; otherwise Enter opens it (when there is something to
	// show). See health_detail.go.
	if m.view == viewDashboard {
		if m.healthDetail {
			return m.handleHealthDetailKey(msg)
		}
		if key.Matches(msg, m.keys.Enter) && m.healthDetailHasContent() {
			m.openHealthDetail()
			return m, nil
		}
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Command):
		m.cmdActive = true
		return m, m.cmd.Focus()
	case key.Matches(msg, m.keys.Dashboard):
		m.view = viewDashboard
		return m, nil
	case key.Matches(msg, m.keys.OSDs):
		m.view = viewOSD
		return m, m.refreshCurrent()
	case key.Matches(msg, m.keys.Pools):
		m.view = viewPool
		return m, m.refreshCurrent()
	case key.Matches(msg, m.keys.Crush):
		m.view = viewCrush
		return m, m.refreshCurrent()
	case key.Matches(msg, m.keys.Flags):
		m.view = viewFlags
		return m, m.refreshCurrent()
	case key.Matches(msg, m.keys.Services):
		m.view = viewServices
		return m, m.refreshCurrent()
	case key.Matches(msg, m.keys.PGs):
		m.view = viewPGs
		return m, m.refreshCurrent()
	}

	return m.delegateKey(msg)
}

// resourceView is the shared contract implemented by every non-dashboard view.
// Routing rendering, sizing, capturing, and the action bar through this
// interface keeps the shell free of a per-view switch for each concern and means
// a new view automatically inherits the shell's chrome once it satisfies this.
type resourceView interface {
	title() string
	actions() []Action
	setSize(width, height int)
	View(width, height int) string
	capturing() bool
	// filterPrompt returns the "/" filter line to render just above the
	// workspace panel, or "" when no filter is open or applied. Views without a
	// filter (e.g. CRUSH) return "".
	filterPrompt() string

	// supportsFilter reports whether the view has a "/" filter, so the global
	// key column can offer it only where it applies (every flat table view, but
	// not the CRUSH tree).
	supportsFilter() bool
}

// activeView returns the sub-model for the current view, or nil for the
// dashboard (which is rendered from state on the Model itself). The returned
// pointer aliases the Model's field, so mutations (e.g. setSize) persist.
func (m *Model) activeView() resourceView {
	switch m.view {
	case viewOSD:
		return &m.osd
	case viewPool:
		return &m.pool
	case viewCrush:
		return &m.crush
	case viewFlags:
		return &m.flags
	case viewServices:
		return &m.services
	case viewPGs:
		return &m.pgs
	default:
		return nil
	}
}

// allViews returns every resource sub-model, for operations that touch all of
// them regardless of which is active (e.g. sizing on a terminal resize).
func (m *Model) allViews() []resourceView {
	return []resourceView{&m.osd, &m.pool, &m.crush, &m.flags, &m.services, &m.pgs}
}

// viewCapturing reports whether the active view has an open input overlay.
func (m Model) viewCapturing() bool {
	if v := m.activeView(); v != nil {
		return v.capturing()
	}
	return false
}

// delegateKey forwards a key to the active view's sub-model.
func (m Model) delegateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m.delegateMsg(msg)
}

// delegateMsg forwards any message to the active view's sub-model.
func (m Model) delegateMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.view {
	case viewOSD:
		m.osd, cmd = m.osd.Update(msg)
	case viewPool:
		m.pool, cmd = m.pool.Update(msg)
	case viewCrush:
		m.crush, cmd = m.crush.Update(msg)
	case viewFlags:
		m.flags, cmd = m.flags.Update(msg)
	case viewServices:
		m.services, cmd = m.services.Update(msg)
	case viewPGs:
		m.pgs, cmd = m.pgs.Update(msg)
	}
	// Opening/closing the "/" filter changes whether a line sits above the panel,
	// so re-size the active view to keep its table height (and scroll maths) in
	// step with the space actually available.
	if v := m.activeView(); v != nil {
		v.setSize(m.contentWidth(), m.contentHeight())
	}
	return m, cmd
}

// runCommand maps a typed command to an action.
func (m Model) runCommand(target string) (tea.Model, tea.Cmd) {
	switch strings.ToLower(target) {
	case "dash", "dashboard":
		m.view = viewDashboard
		return m, nil
	case "osd", "osds":
		m.view = viewOSD
		return m, m.refreshCurrent()
	case "pool", "pools":
		m.view = viewPool
		return m, m.refreshCurrent()
	case "crush":
		m.view = viewCrush
		return m, m.refreshCurrent()
	case "flag", "flags":
		m.view = viewFlags
		return m, m.refreshCurrent()
	case "service", "services", "orch":
		m.view = viewServices
		return m, m.refreshCurrent()
	case "pg", "pgs":
		m.view = viewPGs
		return m, m.refreshCurrent()
	case "q", "quit":
		return m, tea.Quit
	}
	return m, nil
}

// View renders the whole screen through the single layout manager (see
// layout.go).
func (m Model) View() string {
	return m.frame()
}

// dashboardBody renders the dashboard for placement inside the workspace panel.
// When the Health-detail overlay is open it is composited over the grid, keeping
// the dashboard visible behind it (the same pattern the resource views use for
// their popups). While the overlay is open the background comes from the cache
// (dashBG) so scrolling doesn't rebuild the grid every frame; only the overlay
// itself is re-rendered and re-composited.
func (m Model) dashboardBody(width, height int) string {
	if !m.healthDetail {
		return m.renderDashboard()
	}
	bg := m.dashBG
	if bg == "" {
		// Cache miss (shouldn't normally happen while open) — render on demand.
		bg = m.renderDashboard()
	}
	return overlayCenter(bg, m.healthDetailView(), width, height)
}

// renderDashboard renders the dashboard grid (or its loading/error states) at the
// current panel width, without any overlay. It is the single place the grid is
// built, used both directly (no overlay) and to populate the background cache.
func (m Model) renderDashboard() string {
	width := m.contentWidth()
	if m.dash == nil {
		if m.dashErr != nil {
			return errorScreen(m.dashErr)
		}
		return styles.Faint.Render("Loading cluster status…")
	}
	body := views.Dashboard(m.dash, width, m.poolRows)
	if m.dashErr != nil {
		// Keep showing the last good dashboard, with a stale banner on top.
		body = staleBanner(m.dashErr) + "\n\n" + body
	}
	return body
}

// contentWidth/contentHeight give the workspace panel's interior dimensions,
// used to size sub-views on resize. They mirror the arithmetic in frame: the
// panel spans the full width (minus its border) and the height left after the
// separator, the fixed-height header and the panel border.
func (m Model) contentWidth() int {
	w := m.width - 2
	if w < 1 {
		w = 1
	}
	return w
}

func (m Model) contentHeight() int {
	h := m.height - 1 - m.headerHeight() - 2
	// The ":" command prompt and the active view's "/" filter share one line
	// above the panel (never both at once), so this steals at most one row.
	if m.cmdActive {
		h--
	} else if v := m.activeView(); v != nil && v.filterPrompt() != "" {
		h--
	}
	if h < 3 {
		h = 3
	}
	return h
}

// shortID abbreviates an FSID for the header context label.
func shortID(fsid string) string {
	if len(fsid) >= 8 {
		return fsid[:8]
	}
	return fsid
}
