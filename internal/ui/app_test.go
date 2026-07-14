package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cinpol/siphon/internal/ceph/mock"
	"github.com/cinpol/siphon/internal/model"
	"github.com/cinpol/siphon/internal/service"
)

// fold applies a message to the model and returns the concrete type, tidying up
// the very common Update-then-assert dance in these tests.
func fold(m Model, msg tea.Msg) Model {
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func newTestModel(t *testing.T) Model {
	t.Helper()
	m := New(service.New(mock.New()), 5*time.Second, 5, "mock")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return updated.(Model)
}

// TestDashboardRendersMockCluster exercises the full walking skeleton without a
// terminal: mock client -> service -> UI update loop -> rendered view.
func TestDashboardRendersMockCluster(t *testing.T) {
	m := newTestModel(t)

	msg := m.fetchDash()()
	updated, _ := m.Update(msg)
	m = updated.(Model)

	if m.dashErr != nil {
		t.Fatalf("unexpected error after fetch: %v", m.dashErr)
	}

	out := m.View()
	for _, want := range []string{
		"Dashboard",    // nav + panel title (the wordmark is now an ASCII logo)
		"HEALTH_WARN",  // health badge/panel
		"OSD_NEARFULL", // health check
		"tentacle",     // version in header
		"Capacity",     // capacity panel
		"TiB",          // formatted capacity
		"Client IO",    // io panel
		"noout",        // a set flag
		"ec-rgw-data",  // per-pool capacity breakdown (fullest pool)
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard output missing %q\n--- rendered view ---\n%s", want, out)
		}
	}
}

// manyChecksDashboard returns a dashboard whose health detail is far longer than
// the inline preview budget — a busy cluster with a large backlog of pending
// scrubs, the exact case Issue #6 is about.
func manyChecksDashboard() *model.Dashboard {
	details := make([]string, 40)
	for i := range details {
		details[i] = fmt.Sprintf("pg %d.%x not deep-scrubbed since 2026-06-01", i, i)
	}
	return &model.Dashboard{
		Health: model.Health{
			Status: model.HealthWarn,
			Checks: []model.HealthCheck{{
				Code:     "PG_NOT_DEEP_SCRUBBED",
				Severity: "HEALTH_WARN",
				Summary:  "40 pgs not deep-scrubbed in time",
				Details:  details,
			}},
		},
	}
}

// TestHealthDetailScrollableOverlay covers Issue #6: when `ceph health detail`
// output overflows the panel, Enter opens a scrollable overlay (with visible
// key hints), the viewport scrolls, and Esc closes it.
func TestHealthDetailScrollableOverlay(t *testing.T) {
	m := newTestModel(t)
	m = fold(m, dashMsg{dash: manyChecksDashboard()})

	// The inline panel truncates and points at the overlay; the header advertises
	// the action.
	out := m.View()
	if !strings.Contains(out, "more — press enter to view") {
		t.Errorf("expected a truncation hint on the dashboard:\n%s", out)
	}
	if !strings.Contains(out, "Health detail") {
		t.Errorf("expected the Health-detail action hint in the header:\n%s", out)
	}

	// Enter opens the overlay and caches the static dashboard background so a
	// burst of scroll keys doesn't rebuild the grid each frame.
	m = fold(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.healthDetail {
		t.Fatal("expected Enter to open the Health-detail overlay")
	}
	if m.dashBG == "" {
		t.Error("expected the dashboard background to be cached while the overlay is open")
	}
	out = m.View()
	if !strings.Contains(out, "scroll · esc close") {
		t.Errorf("expected scroll key hints in the overlay:\n%s", out)
	}
	// The overlay is composited over the page, so the view must still fill the
	// terminal exactly (footer stays docked, nothing overflows the panel).
	if h := lipgloss.Height(out); h != 40 {
		t.Errorf("overlay-open view height = %d, want 40 (not docked / overflowing)", h)
	}

	// The viewport starts at the top; PgDn scrolls it.
	if off := m.healthVP.YOffset; off != 0 {
		t.Fatalf("overlay should open at the top, got YOffset=%d", off)
	}
	m = fold(m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.healthVP.YOffset == 0 {
		t.Error("expected PgDn to scroll the health-detail viewport")
	}

	// While the overlay is open it captures input: a number key must not switch
	// views out from under it.
	m = fold(m, runes("2"))
	if m.view != viewDashboard || !m.healthDetail {
		t.Error("number keys should be captured while the overlay is open")
	}

	// Esc closes it and drops the cached background.
	m = fold(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.healthDetail {
		t.Error("expected Esc to close the Health-detail overlay")
	}
	if m.dashBG != "" {
		t.Error("expected the cached background to be cleared on close")
	}
}

// TestHealthDetailNoRegressionWhenFew verifies the "no regression" acceptance
// criterion: with only a few items the inline panel shows them in full, with no
// truncation hint.
func TestHealthDetailNoRegressionWhenFew(t *testing.T) {
	m := newTestModel(t)
	m = fold(m, m.fetchDash()()) // mock cluster: two short checks

	out := m.View()
	if strings.Contains(out, "press enter to view") {
		t.Errorf("short health detail should not be truncated:\n%s", out)
	}
	if !strings.Contains(out, "osd.4 is near full") {
		t.Errorf("expected the full detail line to be shown inline:\n%s", out)
	}
}

// TestDashboardPoolRowsConfigurable verifies the configured pool-row count is
// threaded from New() all the way to the rendered dashboard: with rows=2 the
// mock's four pools collapse to the two fullest plus a "+2 more" pointer.
func TestDashboardPoolRowsConfigurable(t *testing.T) {
	m := New(service.New(mock.New()), 5*time.Second, 2, "mock")
	m = fold(m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m = fold(m, m.fetchDash()())

	out := m.View()
	if !strings.Contains(out, "ec-rgw-data") || !strings.Contains(out, "cephfs_data") {
		t.Errorf("expected the two fullest pools with rows=2:\n%s", out)
	}
	if strings.Contains(out, "rbd  ") { // the 3rd-fullest pool should be truncated away
		t.Errorf("rows=2 should not show a third pool row:\n%s", out)
	}
	if !strings.Contains(out, "+2 more pools — press 3 for Pools") {
		t.Errorf("expected a '+2 more pools' pointer with rows=2:\n%s", out)
	}
}

// TestSwitchToOSDView verifies the `2` hotkey switches views and the OSD table
// renders the mock OSDs.
func TestSwitchToOSDView(t *testing.T) {
	m := newTestModel(t)

	// Press "2" to switch to the OSD view; this also triggers a fetch command.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = updated.(Model)
	if m.view != viewOSD {
		t.Fatalf("expected viewOSD after '2', got %v", m.view)
	}
	if cmd == nil {
		t.Fatal("expected a fetch command when entering the OSD view")
	}

	// Run the fetch and fold in the result.
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	if m.osd.err != nil {
		t.Fatalf("unexpected OSD fetch error: %v", m.osd.err)
	}
	out := m.View()
	for _, want := range []string{"OSDs", "HOST", "ceph-01", "up/in"} {
		if !strings.Contains(out, want) {
			t.Errorf("OSD view missing %q\n--- rendered ---\n%s", want, out)
		}
	}
}

// TestSwitchToPoolView verifies the `3` hotkey switches to the pool view and the
// table renders the mock pools.
func TestSwitchToPoolView(t *testing.T) {
	m := newTestModel(t)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	m = updated.(Model)
	if m.view != viewPool {
		t.Fatalf("expected viewPool after '3', got %v", m.view)
	}
	if cmd == nil {
		t.Fatal("expected a fetch command when entering the pool view")
	}

	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if m.pool.err != nil {
		t.Fatalf("unexpected pool fetch error: %v", m.pool.err)
	}
	out := m.View()
	for _, want := range []string{"Pools", "NAME", "rbd", "erasure", "AUTOSCALE"} {
		if !strings.Contains(out, want) {
			t.Errorf("pool view missing %q\n--- rendered ---\n%s", want, out)
		}
	}
}

// TestSwitchToCrushView verifies the `4` hotkey opens the CRUSH tree and it
// renders the hierarchy, and that collapsing a bucket hides its children.
func TestSwitchToCrushView(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	m = updated.(Model)
	if m.view != viewCrush {
		t.Fatalf("expected viewCrush after '4', got %v", m.view)
	}

	// Feed the tree (the view's fetch batches tree + rules).
	updated, _ = m.Update(m.crush.fetchTree()())
	m = updated.(Model)
	if m.crush.err != nil {
		t.Fatalf("unexpected crush error: %v", m.crush.err)
	}

	out := m.View()
	for _, want := range []string{"CRUSH", "default", "rack1", "ceph-01", "osd.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("crush view missing %q\n--- rendered ---\n%s", want, out)
		}
	}
	fullRows := len(m.crush.visible)

	// Cursor starts on the root; collapsing it should hide all descendants.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(Model)
	if len(m.crush.visible) >= fullRows {
		t.Errorf("collapsing root should reduce visible rows: was %d, now %d", fullRows, len(m.crush.visible))
	}
}

// TestStaleDataOnRefreshError verifies that when a refresh fails after data has
// loaded, the view keeps showing the last-good data with a stale banner rather
// than blanking.
func TestStaleDataOnRefreshError(t *testing.T) {
	mc := mock.New()
	m := New(service.New(mc), 5*time.Second, 5, "mock")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(Model)

	// Load the OSD view successfully.
	updated, cmd := m.Update(runes("2"))
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if !strings.Contains(m.View(), "ceph-01") {
		t.Fatal("expected OSDs to load initially")
	}

	// Now make the next refresh fail and trigger it (the auto-refresh path).
	mc.ErrOSDs = errors.New("mon timeout")
	updated, _ = m.Update(m.osd.fetch()())
	m = updated.(Model)

	out := m.View()
	if !strings.Contains(out, "refresh failed") {
		t.Errorf("expected a stale banner after a failed refresh:\n%s", out)
	}
	if !strings.Contains(out, "ceph-01") {
		t.Errorf("expected the last-good OSD data to remain visible:\n%s", out)
	}
}

// TestPGProblemsToggleAndFilter checks the "problems only" toggle and the text
// filter narrow the PG list correctly.
func TestPGProblemsToggleAndFilter(t *testing.T) {
	svc := service.New(mock.New())
	pgs, _ := svc.PGsByPool(context.Background(), "rbd")

	pm := newPGModel(svc)
	updated, _ := pm.Update(pgsMsg{pgs: pgs})
	pm = updated
	if len(pm.visible) != 5 {
		t.Fatalf("want 5 visible PGs, got %d", len(pm.visible))
	}

	// Problems only -> the two non-active+clean PGs.
	updated, _ = pm.Update(runes("u"))
	pm = updated
	if len(pm.visible) != 2 {
		t.Errorf("problems-only want 2, got %d", len(pm.visible))
	}

	// Back to all, then filter by "degraded" -> one PG.
	updated, _ = pm.Update(runes("u"))
	pm = updated
	pm.filter = "degraded"
	pm.rebuild()
	if len(pm.visible) != 1 || pm.visible[0].ID != "2.14" {
		t.Errorf("filter 'degraded' want [2.14], got %+v", pm.visible)
	}
}

// TestOpErrorClearsOnRefresh verifies a failed-operation banner is transient: it
// shows after the failure and clears on the next successful data load (so it no
// longer persists until the app is restarted).
func TestOpErrorClearsOnRefresh(t *testing.T) {
	pm := newPoolModel(service.New(mock.New()))
	pm, _ = pm.Update(pm.fetch()()) // load pools

	pm, _ = pm.Update(opResultMsg{err: errors.New("mon command osd pool delete: not permitted")})
	if pm.opErr == nil {
		t.Fatal("expected opErr to be set after a failed operation")
	}
	if !strings.Contains(pm.View(90, 20), "operation failed") {
		t.Errorf("expected the error banner to render:\n%s", pm.View(90, 20))
	}

	pm, _ = pm.Update(pm.fetch()()) // a refresh
	if pm.opErr != nil {
		t.Errorf("expected opErr to clear on refresh, got %v", pm.opErr)
	}
}

// TestServiceDrillAndRestart drills into a service's daemons and restarts the
// service via y/N, checking the orchestrator command is dispatched.
func TestServiceDrillAndRestart(t *testing.T) {
	mc := mock.New()
	m := New(service.New(mc), 5*time.Second, 5, "mock")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(Model)

	updated, cmd := m.Update(runes("6"))
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	// Enter drills directly into the selected service's daemons (no action menu).
	updated, drill := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.services.level != svcLevelDaemons {
		t.Fatal("expected Enter to drill into the daemons level")
	}
	if drill == nil {
		t.Fatal("expected a fetch command after drilling in")
	}
	updated, _ = m.Update(drill())
	m = updated.(Model)
	if len(m.services.daemons) == 0 {
		t.Fatal("expected daemons to load after drill-down")
	}

	// Back to services, restart the selected service (mon) via `r` + y/n.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	updated, _ = m.Update(runes("r"))
	m = updated.(Model)
	if !m.services.confirming {
		t.Fatal("expected a confirmation after 'r'")
	}
	updated, runCmd := m.Update(runes("y"))
	m = updated.(Model)
	if runCmd == nil {
		t.Fatal("expected an execution command after y")
	}
	m.Update(runCmd())
	if got := mc.LastCommand["prefix"]; got != "orch restart" {
		t.Errorf("expected 'orch restart' dispatched, got %v", got)
	}
}

// TestFlagToggleFlow verifies toggling a flag: open the flags view, select an
// unset flag, press `t` to toggle, confirm with `y`, and check the command
// dispatched and the flag is now set after refresh.
func TestFlagToggleFlow(t *testing.T) {
	mc := mock.New()
	m := New(service.New(mc), 5*time.Second, 5, "mock")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(Model)

	updated, cmd := m.Update(runes("5"))
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	// nobackfill is at index 2 and starts unset.
	for i := 0; i < 2; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
	}
	if f, _ := m.flags.selected(); f.Name != "nobackfill" {
		t.Fatalf("expected cursor on nobackfill, got %q", f.Name)
	}

	// Press `e` to enable nobackfill (unset), starting the y/n confirm.
	updated, _ = m.Update(runes("e"))
	m = updated.(Model)
	if !m.flags.confirming {
		t.Fatal("expected a y/n confirmation after pressing 'e'")
	}

	updated, runCmd := m.Update(runes("y"))
	m = updated.(Model)
	if runCmd == nil {
		t.Fatal("expected an execution command after pressing y")
	}
	updated, refetch := m.Update(runCmd())
	m = updated.(Model)

	if got := mc.LastCommand["prefix"]; got != "osd set" {
		t.Fatalf("expected 'osd set' dispatched, got %v", got)
	}
	if refetch != nil {
		updated, _ = m.Update(refetch())
		m = updated.(Model)
	}
	if !m.flags.set["nobackfill"] {
		t.Error("expected nobackfill to be set after the toggle")
	}
}

// TestCrushMoveFlow drives the 4-step move: select a host, press Move, pick a
// destination rack, confirm, and verify the mock CRUSH map reflects the move.
func TestCrushMoveFlow(t *testing.T) {
	mc := mock.New()
	m := New(service.New(mc), 5*time.Second, 5, "mock")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	m = updated.(Model)
	updated, _ = m.Update(m.crush.fetchTree()())
	m = updated.(Model)

	// Navigate to ceph-02 (visible index 5) and open the move picker.
	for i := 0; i < 5; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(Model)
	}
	if n, _ := m.crush.selectedNode(); n.Name != "ceph-02" {
		t.Fatalf("expected cursor on ceph-02, got %q", n.Name)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = updated.(Model)
	if m.crush.mode != crushPick {
		t.Fatal("expected the move destination picker to open")
	}

	// Pick the second destination (rack2) and confirm.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.crush.mode != crushConfirm {
		t.Fatalf("expected confirmation, got mode %v", m.crush.mode)
	}
	updated, runCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(Model)
	if runCmd == nil {
		t.Fatal("expected an execution command after confirming the move")
	}
	m.Update(runCmd()) // dispatch to the mock

	// The mock CRUSH map should now have ceph-02 (-5) under rack2 (-3).
	nodes, _ := mc.CrushTree(nil)
	for _, n := range nodes {
		if n.ID == -3 { // rack2
			found := false
			for _, c := range n.Children {
				if c == -5 {
					found = true
				}
			}
			if !found {
				t.Errorf("expected ceph-02 (-5) under rack2, children=%v", n.Children)
			}
		}
	}
}

// TestPoolCreateFlow drives the create-pool workflow: open the pool view, `c`,
// fill the name, submit, confirm with `y`, and verify the pool is created and
// appears after refresh.
func TestPoolCreateFlow(t *testing.T) {
	mc := mock.New()
	m := New(service.New(mc), 5*time.Second, 5, "mock")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(Model)

	// Enter pool view and load pools.
	updated, cmd := m.Update(runes("3"))
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	// Open the create form.
	updated, _ = m.Update(runes("c"))
	m = updated.(Model)
	if m.pool.mode != poolForm {
		t.Fatal("expected the create form to open after 'c'")
	}

	// Type the pool name (first field is focused) and submit.
	for _, r := range "backups" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.pool.mode != poolConfirm {
		t.Fatalf("expected confirmation after submitting the form, got mode %v", m.pool.mode)
	}

	// Confirm with `y`.
	updated, runCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = updated.(Model)
	if runCmd == nil {
		t.Fatal("expected an execution command after confirming")
	}

	// Execute the operation and fold in the resulting refetch.
	updated, refetch := m.Update(runCmd())
	m = updated.(Model)
	if refetch != nil {
		updated, _ = m.Update(refetch())
		m = updated.(Model)
	}

	found := false
	for _, p := range m.pool.pools {
		if p.Name == "backups" {
			found = true
		}
	}
	if !found {
		t.Error("expected the 'backups' pool to appear after creation")
	}
}

// TestCommandPromptSwitchesView verifies `:osd` navigates via the command prompt.
func TestCommandPromptSwitchesView(t *testing.T) {
	m := newTestModel(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = updated.(Model)
	if !m.cmdActive {
		t.Fatal("expected command prompt to open on ':'")
	}
	for _, r := range "osd" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.cmdActive {
		t.Error("command prompt should close after Enter")
	}
	if m.view != viewOSD {
		t.Errorf("expected viewOSD after ':osd', got %v", m.view)
	}
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// TestOSDOutOperationFlow drives the full y/n confirmation workflow: open the
// OSD view, press `o`, verify `n` cancels, then confirm with `y` and verify the
// correct command reached the cluster and the OSD's state updated on refresh.
func TestOSDOutOperationFlow(t *testing.T) {
	mc := mock.New()
	m := New(service.New(mc), 5*time.Second, 5, "mock")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updated.(Model)

	// Enter the OSD view and load the OSDs (cursor lands on osd.0, which is in).
	updated, cmd := m.Update(runes("2"))
	m = updated.(Model)
	updated, _ = m.Update(cmd())
	m = updated.(Model)

	// Press `o` -> confirm dialog opens and captures input.
	updated, _ = m.Update(runes("o"))
	m = updated.(Model)
	if !m.osd.capturing() {
		t.Fatal("expected the confirm dialog to open and capture input after 'o'")
	}

	// `n` must cancel without executing anything.
	updated, _ = m.Update(runes("n"))
	m = updated.(Model)
	if mc.LastCommand != nil {
		t.Fatalf("declining the confirmation should not run a command, got %v", mc.LastCommand)
	}
	if m.osd.capturing() {
		t.Fatal("dialog should close after declining with 'n'")
	}

	// Reopen and confirm with `y`.
	updated, _ = m.Update(runes("o"))
	m = updated.(Model)
	updated, runCmd := m.Update(runes("y"))
	m = updated.(Model)
	if runCmd == nil {
		t.Fatal("expected an execution command after confirming with 'y'")
	}

	// Executing the command dispatches to the cluster and yields a refetch cmd.
	updated, refetch := m.Update(runCmd())
	m = updated.(Model)
	if got := mc.LastCommand["prefix"]; got != "osd out" {
		t.Fatalf("expected 'osd out' to be dispatched, got %v", got)
	}
	if refetch != nil {
		updated, _ = m.Update(refetch())
		m = updated.(Model)
	}
	if m.osd.osds[0].In {
		t.Error("osd.0 should be marked out after the operation")
	}
}

// TestOSDFilterFlow drives the "/" live filter: open OSDs, filter down to a
// single host, and verify (a) the list narrows live, (b) an action taken on the
// filtered selection targets the right OSD — proving the displayed-row→source
// mapping — and (c) Esc restores the full list.
func TestOSDFilterFlow(t *testing.T) {
	mc := mock.New()
	m := New(service.New(mc), 5*time.Second, 5, "mock")
	m = fold(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m = fold(m, runes("2"))
	m = fold(m, m.osd.fetch()())

	// Open the filter and type a host only OSD.4 lives on.
	m = fold(m, runes("/"))
	if !m.osd.capturing() {
		t.Fatal("expected the view to capture input while the filter prompt is open")
	}
	for _, r := range "ceph-03" {
		m = fold(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.View(); !strings.Contains(got, "ceph-03") || strings.Contains(got, "ceph-01") {
		t.Errorf("filter did not narrow to ceph-03 only:\n%s", got)
	}

	// Commit: the prompt closes but the list stays filtered.
	m = fold(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.osd.capturing() {
		t.Fatal("committing the filter should stop capturing input")
	}
	if o, ok := m.osd.selectedOSD(); !ok || o.ID != 4 {
		t.Fatalf("filtered selection should map to OSD.4, got %+v (ok=%v)", o, ok)
	}

	// Mark the filtered OSD in and confirm with y; the command must target OSD.4.
	m = fold(m, runes("i"))
	if m.osd.mode != osdConfirm {
		t.Fatalf("expected a confirmation after 'i', mode=%v", m.osd.mode)
	}
	updated, runCmd := m.Update(runes("y"))
	m = updated.(Model)
	if runCmd == nil {
		t.Fatal("expected an execution command after confirming")
	}
	m.Update(runCmd())
	if got := mc.LastCommand["prefix"]; got != "osd in" {
		t.Fatalf("expected 'osd in' to be dispatched, got %v", got)
	}
	if ids, _ := mc.LastCommand["ids"].([]string); len(ids) != 1 || ids[0] != "4" {
		t.Fatalf("expected the action to target OSD.4, got ids=%v", mc.LastCommand["ids"])
	}

	// Esc clears the filter and restores the full list.
	m = fold(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.osd.filter.applied() {
		t.Error("esc should clear the applied filter")
	}
	if got := m.View(); !strings.Contains(got, "ceph-01") {
		t.Errorf("full OSD list should return after clearing the filter:\n%s", got)
	}
}

// TestFooterDockedAtBottom verifies the rendered view always fills exactly the
// terminal height — proving the footer stays docked to the bottom regardless of
// view or content length (a tall dashboard vs a short OSD list) and terminal
// size.
func TestFooterDockedAtBottom(t *testing.T) {
	sizes := []struct {
		name string
		w, h int
	}{
		{"small", 80, 24},
		{"standard", 100, 40},
		{"tall", 120, 50},
	}
	for _, s := range sizes {
		t.Run(s.name, func(t *testing.T) {
			m := New(service.New(mock.New()), 5*time.Second, 5, "mock")
			m = fold(m, tea.WindowSizeMsg{Width: s.w, Height: s.h})
			m = fold(m, m.fetchDash()())

			// Dashboard (content-heavy view).
			if got := lipgloss.Height(m.View()); got != s.h {
				t.Errorf("dashboard: view height = %d, want %d (footer not docked)", got, s.h)
			}

			// OSD view (short content — the case that used to float the footer up).
			m2 := fold(m, runes("2"))
			m2 = fold(m2, m2.osd.fetch()())
			if got := lipgloss.Height(m2.View()); got != s.h {
				t.Errorf("osd: view height = %d, want %d (footer not docked)", got, s.h)
			}
		})
	}
}

// TestActionBarPerView verifies the header's action columns show the active
// view's context-sensitive actions.
func TestActionBarPerView(t *testing.T) {
	m := newTestModel(t)
	m = fold(m, m.fetchDash()())

	cases := []struct {
		key  string
		want []string
	}{
		{"2", []string{"Out", "Destroy", "Purge", "Remove"}}, // OSD
		{"3", []string{"Create", "Edit", "Delete"}},          // Pool
		{"4", []string{"Move", "Rename", "Delete", "Rules"}}, // CRUSH
		{"6", []string{"Restart"}},                           // Services
	}
	for _, c := range cases {
		mv := fold(m, runes(c.key))
		if cmd := mv.refreshCurrent(); cmd != nil {
			mv = fold(mv, cmd())
		}
		out := mv.View()
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Errorf("view %q action bar missing %q\n%s", c.key, w, out)
			}
		}
	}
}

// TestOSDEnterOpensDetail verifies the no-menu interaction model: Enter opens
// the OSD detail view (with the page still visible behind it), and destructive
// actions are triggered directly by their shortcut key.
func TestOSDEnterOpensDetail(t *testing.T) {
	m := newTestModel(t)
	updated, cmd := m.Update(runes("2"))
	m = updated.(Model)
	m = fold(m, cmd())

	// Enter opens the OSD detail view — no action menu.
	m = fold(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.osd.detail {
		t.Fatal("expected Enter to open the OSD detail view")
	}
	got := m.View()
	if !strings.Contains(got, "crush wt") { // a detail-only field
		t.Errorf("expected OSD detail content:\n%s", got)
	}
	if !strings.Contains(got, "ceph-01") { // the list stays visible behind the popup
		t.Errorf("expected the OSD list to remain visible behind the detail popup:\n%s", got)
	}

	// Esc closes the detail view.
	m = fold(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.osd.detail {
		t.Fatal("expected Esc to close the detail view")
	}

	// A destructive action is triggered directly by its shortcut, not a menu.
	m = fold(m, runes("d")) // destroy
	if m.osd.mode != osdConfirm {
		t.Fatalf("expected a confirmation after 'd', mode=%v", m.osd.mode)
	}
	if got := m.View(); !strings.Contains(got, "destroy") {
		t.Errorf("confirm should preview the destroy command:\n%s", got)
	}
}

// TestQuitKeys verifies the app requests shutdown on the documented quit keys.
func TestQuitKeys(t *testing.T) {
	m := newTestModel(t)

	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}); cmd == nil {
		t.Error(`key "q" did not produce a command (expected Quit)`)
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Error("ctrl+c did not produce a command (expected Quit)")
	}
}
