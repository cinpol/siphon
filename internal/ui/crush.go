package ui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cinpol/argonaut/internal/model"
	"github.com/cinpol/argonaut/internal/service"
	"github.com/cinpol/argonaut/internal/ui/components"
	"github.com/cinpol/argonaut/internal/ui/styles"
)

// crushMode is the CRUSH view's interaction state.
type crushMode int

const (
	crushNormal crushMode = iota
	crushPick             // choosing a move destination
	crushForm             // creating a bucket
	crushPrompt           // single-value input (rename / reweight / class)
	crushConfirm
)

// crushPromptKind distinguishes what a single-value prompt is collecting.
type crushPromptKind int

const (
	promptRename crushPromptKind = iota
	promptReweight
	promptClass
)

// bucketTypes are the CRUSH bucket types offered when creating a bucket.
var bucketTypes = []string{"rack", "host", "row", "room", "datacenter", "root"}

type crushKeyMap struct {
	Nav      key.Binding
	Expand   key.Binding
	Collapse key.Binding
	Toggle   key.Binding
	Rules    key.Binding
	Move     key.Binding
	Create   key.Binding
	Rename   key.Binding
	Delete   key.Binding
	Reweight key.Binding
	Class    key.Binding
	Back     key.Binding
}

func defaultCrushKeys() crushKeyMap {
	return crushKeyMap{
		Nav:      key.NewBinding(key.WithKeys("up", "down", "j", "k"), key.WithHelp("↑/↓", "navigate")),
		Expand:   key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→", "expand")),
		Collapse: key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←", "collapse")),
		Toggle:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "expand/collapse")),
		Rules:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "rules")),
		Move:     key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "move")),
		Create:   key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new bucket")),
		Rename:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rename")),
		Delete:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		Reweight: key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "reweight")),
		Class:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "device-class")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
}

type crushRow struct {
	id         int
	depth      int
	expandable bool
}

// crushDest is a candidate destination bucket for a move.
type crushDest struct {
	Type   string
	Name   string
	TypeID int
}

type crushModel struct {
	svc  *service.Service
	keys crushKeyMap

	nodes    map[int]model.CrushNode
	parent   map[int]int
	weight   map[int]float64
	roots    []int
	expanded map[int]bool
	visible  []crushRow
	cursor   int
	offset   int

	rules     []model.CrushRule
	showRules bool

	mode       crushMode
	picker     []crushDest
	pickerAt   int
	pickerNode model.CrushNode
	form       components.Form
	prompt     textinput.Model
	promptKind crushPromptKind
	promptNode model.CrushNode
	promptErr  bool
	confirm    components.Confirm
	pendingOp  service.Operation
	opErr      error
	notice     string

	err     error
	loading bool
	width   int
	height  int
}

func newCrushModel(svc *service.Service) crushModel {
	return crushModel{svc: svc, keys: defaultCrushKeys(), expanded: map[int]bool{}, loading: true}
}

type (
	crushMsg      struct{ nodes []model.CrushNode }
	crushRulesMsg struct{ rules []model.CrushRule }
	crushErrMsg   struct{ err error }
)

func (m crushModel) fetch() tea.Cmd {
	return tea.Batch(m.fetchTree(), m.fetchRules())
}

func (m crushModel) fetchTree() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		nodes, err := m.svc.CrushTree(ctx)
		if err != nil {
			return crushErrMsg{err}
		}
		return crushMsg{nodes}
	}
}

func (m crushModel) fetchRules() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()
		rules, err := m.svc.CrushRules(ctx)
		if err != nil {
			return crushErrMsg{err}
		}
		return crushRulesMsg{rules}
	}
}

func (m crushModel) capturing() bool { return m.mode != crushNormal }

func (m crushModel) title() string { return "CRUSH" }

// filterPrompt satisfies the resourceView interface. The CRUSH view is a tree,
// not a flat table, so it has no "/" row filter (tree filtering is a separate
// design); it always returns "".
func (m crushModel) filterPrompt() string { return "" }

// supportsFilter reports that CRUSH has no "/" filter.
func (m crushModel) supportsFilter() bool { return false }

func (m crushModel) actions() []Action {
	return []Action{
		act(m.keys.Move, false),
		act(m.keys.Create, false),
		act(m.keys.Rename, false),
		act(m.keys.Reweight, false),
		act(m.keys.Class, false),
		act(m.keys.Delete, true),
		act(m.keys.Rules, false),
	}
}

func (m *crushModel) setSize(width, height int) { m.width, m.height = width, height }

func (m crushModel) Update(msg tea.Msg) (crushModel, tea.Cmd) {
	switch msg := msg.(type) {
	case crushMsg:
		m.err = nil
		m.opErr = nil // a fresh, successful load clears any stale op error
		m.loading = false
		m.build(msg.nodes)
		return m, nil
	case crushRulesMsg:
		m.rules = msg.rules
		return m, nil
	case crushErrMsg:
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
		return m, m.fetchTree()
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	switch m.mode {
	case crushForm:
		m.form, cmd = m.form.Update(msg)
	case crushPrompt:
		m.prompt, cmd = m.prompt.Update(msg)
	case crushConfirm:
		m.confirm, cmd = m.confirm.Update(msg)
	}
	return m, cmd
}

func (m crushModel) handleKey(msg tea.KeyMsg) (crushModel, tea.Cmd) {
	switch m.mode {
	case crushPick:
		return m.handlePickKey(msg)
	case crushForm:
		return m.handleFormKey(msg)
	case crushPrompt:
		return m.handlePromptKey(msg)
	case crushConfirm:
		return m.handleConfirmKey(msg)
	}
	return m.handleNormalKey(msg)
}

func (m crushModel) handleNormalKey(msg tea.KeyMsg) (crushModel, tea.Cmd) {
	if m.showRules {
		if key.Matches(msg, m.keys.Back) || key.Matches(msg, m.keys.Rules) {
			m.showRules = false
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Rules):
		m.showRules = true
	case key.Matches(msg, m.keys.Nav):
		if msg.String() == "up" || msg.String() == "k" {
			m.moveCursor(-1)
		} else {
			m.moveCursor(1)
		}
	case key.Matches(msg, m.keys.Collapse):
		m.collapseOrParent()
	case key.Matches(msg, m.keys.Expand):
		m.expandOrChild()
	case key.Matches(msg, m.keys.Toggle):
		m.toggleExpand()
	case key.Matches(msg, m.keys.Move):
		return m.startMove()
	case key.Matches(msg, m.keys.Create):
		return m.startCreate()
	case key.Matches(msg, m.keys.Rename):
		return m.startPrompt(promptRename)
	case key.Matches(msg, m.keys.Reweight):
		return m.startPrompt(promptReweight)
	case key.Matches(msg, m.keys.Class):
		return m.startPrompt(promptClass)
	case key.Matches(msg, m.keys.Delete):
		return m.startDelete()
	}
	return m, nil
}

// --- operation starters ---------------------------------------------------

func (m crushModel) startMove() (crushModel, tea.Cmd) {
	node, ok := m.selectedNode()
	if !ok {
		return m, nil
	}
	if node.Type == "root" {
		m.notice = "root buckets cannot be moved"
		return m, nil
	}
	dests := m.validDestinations(node)
	if len(dests) == 0 {
		m.notice = "no valid destination for this node"
		return m, nil
	}
	m.pickerNode = node
	m.picker = dests
	m.pickerAt = 0
	m.mode = crushPick
	m.notice = ""
	return m, nil
}

func (m crushModel) startCreate() (crushModel, tea.Cmd) {
	m.form = components.NewForm("Create CRUSH bucket",
		components.TextField("Name", "rack3", ""),
		components.ChoiceField("Type", bucketTypes, 0),
	)
	m.mode = crushForm
	m.notice = ""
	return m, textinput.Blink
}

func (m crushModel) startPrompt(kind crushPromptKind) (crushModel, tea.Cmd) {
	node, ok := m.selectedNode()
	if !ok {
		return m, nil
	}
	switch kind {
	case promptRename:
		if node.IsOSD() {
			m.notice = "only buckets can be renamed"
			return m, nil
		}
	case promptClass:
		if !node.IsOSD() {
			m.notice = "device class applies to OSDs only"
			return m, nil
		}
	case promptReweight:
		if node.Type == "root" {
			m.notice = "root buckets cannot be reweighted"
			return m, nil
		}
	}

	ti := textinput.New()
	ti.CharLimit = 32
	switch kind {
	case promptRename:
		ti.SetValue(node.Name)
	case promptReweight:
		ti.SetValue(fmt.Sprintf("%.4f", m.weight[node.ID]))
	case promptClass:
		ti.SetValue(node.DeviceClass)
	}
	ti.Focus()

	m.prompt = ti
	m.promptKind = kind
	m.promptNode = node
	m.promptErr = false
	m.mode = crushPrompt
	m.notice = ""
	return m, textinput.Blink
}

func (m crushModel) startDelete() (crushModel, tea.Cmd) {
	node, ok := m.selectedNode()
	if !ok {
		return m, nil
	}
	if node.IsOSD() {
		m.notice = "remove OSDs from the OSD view, not the tree"
		return m, nil
	}
	if node.Type == "root" {
		m.notice = "root buckets cannot be deleted"
		return m, nil
	}
	return m.startConfirm(m.svc.CrushRemoveBucket(node.Name))
}

// --- mode key handlers ----------------------------------------------------

func (m crushModel) handlePickKey(msg tea.KeyMsg) (crushModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.pickerAt > 0 {
			m.pickerAt--
		}
	case "down", "j":
		if m.pickerAt < len(m.picker)-1 {
			m.pickerAt++
		}
	case "enter":
		dest := m.picker[m.pickerAt]
		return m.startConfirm(m.svc.CrushMove(m.pickerNode.Name, dest.Type, dest.Name))
	case "esc":
		m.mode = crushNormal
	}
	return m, nil
}

func (m crushModel) handleFormKey(msg tea.KeyMsg) (crushModel, tea.Cmd) {
	var cmd tea.Cmd
	m.form, cmd = m.form.Update(msg)
	switch m.form.State() {
	case components.FormSubmitted:
		name := m.form.Value(0)
		if name == "" {
			m.form = m.form.Reactivate()
			m.notice = "bucket name is required"
			return m, nil
		}
		return m.startConfirm(m.svc.CrushCreateBucket(name, m.form.Value(1)))
	case components.FormCancelled:
		m.mode = crushNormal
		return m, nil
	}
	return m, cmd
}

func (m crushModel) handlePromptKey(msg tea.KeyMsg) (crushModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		val := strings.TrimSpace(m.prompt.Value())
		switch m.promptKind {
		case promptRename:
			if val == "" {
				m.promptErr = true
				return m, nil
			}
			m.prompt.Blur()
			return m.startConfirm(m.svc.CrushRenameBucket(m.promptNode.Name, val))
		case promptClass:
			if val == "" {
				m.promptErr = true
				return m, nil
			}
			m.prompt.Blur()
			return m.startConfirm(m.svc.CrushSetDeviceClass(m.promptNode.Name, val))
		case promptReweight:
			w, err := strconv.ParseFloat(val, 64)
			if err != nil || w < 0 {
				m.promptErr = true
				return m, nil
			}
			m.prompt.Blur()
			return m.startConfirm(m.svc.CrushReweight(m.promptNode.Name, w))
		}
	case "esc":
		m.mode = crushNormal
		m.prompt.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(msg)
	return m, cmd
}

func (m crushModel) handleConfirmKey(msg tea.KeyMsg) (crushModel, tea.Cmd) {
	var cmd tea.Cmd
	m.confirm, cmd = m.confirm.Update(msg)
	switch m.confirm.State() {
	case components.ConfirmAccepted:
		m.mode = crushNormal
		return m, runOp(m.pendingOp)
	case components.ConfirmCancelled:
		m.mode = crushNormal
		return m, nil
	}
	return m, cmd
}

func (m crushModel) startConfirm(op service.Operation) (crushModel, tea.Cmd) {
	m.pendingOp = op
	m.confirm = components.NewYesNo(op.Title, op.Command, op.Consequence, op.Irreversible)
	m.mode = crushConfirm
	m.notice = ""
	return m, nil
}

// validDestinations returns the buckets a node may legally move under: a bucket
// of a higher CRUSH type that is neither the node itself nor one of its
// descendants.
func (m crushModel) validDestinations(node model.CrushNode) []crushDest {
	desc := m.descendants(node.ID)
	var dests []crushDest
	for _, n := range m.nodes {
		if n.IsOSD() || n.ID == node.ID || desc[n.ID] {
			continue
		}
		if n.TypeID <= node.TypeID {
			continue
		}
		dests = append(dests, crushDest{Type: n.Type, Name: n.Name, TypeID: n.TypeID})
	}
	sort.Slice(dests, func(i, j int) bool {
		if dests[i].TypeID != dests[j].TypeID {
			return dests[i].TypeID < dests[j].TypeID
		}
		return dests[i].Name < dests[j].Name
	})
	return dests
}

func (m crushModel) descendants(id int) map[int]bool {
	out := map[int]bool{}
	var walk func(id int)
	walk = func(id int) {
		for _, c := range m.nodes[id].Children {
			if !out[c] {
				out[c] = true
				walk(c)
			}
		}
	}
	walk(id)
	return out
}

// --- tree construction (shared with 4a) -----------------------------------

func (m *crushModel) build(nodes []model.CrushNode) {
	m.nodes = make(map[int]model.CrushNode, len(nodes))
	m.parent = make(map[int]int, len(nodes))
	isChild := make(map[int]bool, len(nodes))
	for _, n := range nodes {
		m.nodes[n.ID] = n
	}
	for _, n := range nodes {
		for _, c := range n.Children {
			m.parent[c] = n.ID
			isChild[c] = true
		}
	}
	m.roots = nil
	for _, n := range nodes {
		if !isChild[n.ID] {
			m.roots = append(m.roots, n.ID)
		}
	}
	if len(m.expanded) == 0 {
		for _, n := range nodes {
			if !n.IsOSD() {
				m.expanded[n.ID] = true
			}
		}
	}
	m.computeWeights()
	m.rebuild()
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *crushModel) computeWeights() {
	m.weight = make(map[int]float64, len(m.nodes))
	var walk func(id int) float64
	walk = func(id int) float64 {
		n, ok := m.nodes[id]
		if !ok {
			return 0
		}
		if n.IsOSD() {
			m.weight[id] = n.CrushWeight
			return n.CrushWeight
		}
		sum := 0.0
		for _, c := range n.Children {
			sum += walk(c)
		}
		m.weight[id] = sum
		return sum
	}
	for _, r := range m.roots {
		walk(r)
	}
}

func (m *crushModel) rebuild() {
	m.visible = nil
	var walk func(id, depth int)
	walk = func(id, depth int) {
		n, ok := m.nodes[id]
		if !ok {
			return
		}
		expandable := !n.IsOSD() && len(n.Children) > 0
		m.visible = append(m.visible, crushRow{id: id, depth: depth, expandable: expandable})
		if expandable && m.expanded[id] {
			for _, c := range n.Children {
				walk(c, depth+1)
			}
		}
	}
	for _, r := range m.roots {
		walk(r, 0)
	}
}

func (m *crushModel) moveCursor(d int) {
	m.cursor += d
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
}

func (m *crushModel) toggleExpand() {
	if row, ok := m.currentRow(); ok && row.expandable {
		m.expanded[row.id] = !m.expanded[row.id]
		m.rebuild()
	}
}

func (m *crushModel) expandOrChild() {
	row, ok := m.currentRow()
	if !ok {
		return
	}
	if row.expandable && !m.expanded[row.id] {
		m.expanded[row.id] = true
		m.rebuild()
		return
	}
	m.moveCursor(1)
}

func (m *crushModel) collapseOrParent() {
	row, ok := m.currentRow()
	if !ok {
		return
	}
	if row.expandable && m.expanded[row.id] {
		m.expanded[row.id] = false
		m.rebuild()
		return
	}
	if pid, ok := m.parent[row.id]; ok {
		for i, r := range m.visible {
			if r.id == pid {
				m.cursor = i
				return
			}
		}
	}
}

func (m crushModel) currentRow() (crushRow, bool) {
	if m.cursor >= 0 && m.cursor < len(m.visible) {
		return m.visible[m.cursor], true
	}
	return crushRow{}, false
}

func (m crushModel) selectedNode() (model.CrushNode, bool) {
	if row, ok := m.currentRow(); ok {
		n, ok := m.nodes[row.id]
		return n, ok
	}
	return model.CrushNode{}, false
}

// --- rendering ------------------------------------------------------------

func (m crushModel) View(width, height int) string {
	bg := m.treeView(width, height)

	// Edit dialogs and the rules list are composited over the tree so the
	// hierarchy stays visible behind them.
	switch m.mode {
	case crushPick:
		return overlayCenter(bg, m.pickerView(width), width, height)
	case crushForm:
		return overlayCenter(bg, m.form.View(width), width, height)
	case crushPrompt:
		return overlayCenter(bg, m.promptView(width), width, height)
	case crushConfirm:
		return overlayCenter(bg, m.confirm.View(width), width, height)
	}
	if m.showRules {
		return overlayCenter(bg, m.rulesView(width), width, height)
	}
	return bg
}

// treeView renders the CRUSH hierarchy (the background behind any dialog).
func (m crushModel) treeView(width, height int) string {
	if m.err != nil && len(m.visible) == 0 {
		return errorScreen(m.err)
	}
	switch {
	case m.loading && len(m.visible) == 0:
		return styles.Faint.Render("Loading CRUSH tree…")
	case len(m.visible) == 0:
		return styles.Faint.Render("Empty CRUSH tree.")
	}

	prefix := ""
	switch {
	case m.err != nil:
		prefix = staleBanner(m.err) + "\n\n"
	case m.opErr != nil:
		prefix = styles.Danger.Render("operation failed: "+m.opErr.Error()) + "\n\n"
	case m.notice != "":
		prefix = styles.Faint.Render(m.notice) + "\n\n"
	}

	rows := height - strings.Count(prefix, "\n")
	if rows < 1 {
		rows = 1
	}
	offset := m.offset
	if m.cursor < offset {
		offset = m.cursor
	}
	if m.cursor >= offset+rows {
		offset = m.cursor - rows + 1
	}
	end := offset + rows
	if end > len(m.visible) {
		end = len(m.visible)
	}

	var b strings.Builder
	b.WriteString(prefix)
	for i := offset; i < end; i++ {
		b.WriteString(m.renderRow(m.visible[i], i == m.cursor))
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func (m crushModel) renderRow(row crushRow, selected bool) string {
	n := m.nodes[row.id]
	icon := " "
	if row.expandable {
		if m.expanded[row.id] {
			icon = "▾"
		} else {
			icon = "▸"
		}
	}
	name := strings.Repeat("  ", row.depth) + icon + " " + n.Name
	class := ""
	if n.IsOSD() {
		class = n.DeviceClass
	}
	line := fmt.Sprintf("%-30s %-9s %-5s w %7.2f", truncate(name, 30), n.Type, class, m.weight[row.id])
	if selected {
		return lipgloss.NewStyle().Bold(true).
			Foreground(styles.SelectedFg()).
			Background(styles.SelectedBg()).
			Render(line)
	}
	return line
}

func (m crushModel) pickerView(width int) string {
	var b strings.Builder
	b.WriteString(styles.PanelTitle.Render(fmt.Sprintf("Move %s %q to:", m.pickerNode.Type, m.pickerNode.Name)) + "\n\n")
	for i, d := range m.picker {
		marker := styles.Faint.Render("( )")
		label := fmt.Sprintf("%s  %s", d.Name, styles.Faint.Render("("+d.Type+")"))
		if i == m.pickerAt {
			marker = styles.Accent.Render("(•)")
			label = lipgloss.NewStyle().Bold(true).Render(d.Name) + "  " + styles.Faint.Render("("+d.Type+")")
		}
		b.WriteString(marker + " " + label + "\n")
	}
	b.WriteString("\n" + styles.Faint.Render("[↑/↓] choose   [enter] select   [esc] cancel"))
	return crushBox(b.String(), width)
}

func (m crushModel) promptView(width int) string {
	labels := map[crushPromptKind]string{
		promptRename:   fmt.Sprintf("Rename bucket %q", m.promptNode.Name),
		promptReweight: fmt.Sprintf("Reweight %s", m.promptNode.Name),
		promptClass:    fmt.Sprintf("Set device class for %s", m.promptNode.Name),
	}
	var b strings.Builder
	b.WriteString(styles.PanelTitle.Render(labels[m.promptKind]) + "\n\n")
	b.WriteString(m.prompt.View())
	if m.promptErr {
		b.WriteString("\n" + styles.Danger.Render("invalid value"))
	}
	b.WriteString("\n\n" + styles.Faint.Render("[enter] continue   [esc] cancel"))
	return crushBox(b.String(), width)
}

func (m crushModel) rulesView(width int) string {
	var b strings.Builder
	b.WriteString(styles.PanelTitle.Render("CRUSH rules") + "\n")
	if len(m.rules) == 0 {
		b.WriteString(styles.Faint.Render("no rules"))
	}
	for _, r := range m.rules {
		b.WriteString(fmt.Sprintf("\n%s %s\n", styles.Label.Render(r.Name), styles.Faint.Render("("+r.Type+")")))
		for _, s := range r.Steps {
			b.WriteString("  " + styles.Faint.Render(s) + "\n")
		}
	}
	b.WriteString("\n" + styles.Faint.Render("[esc] back"))
	return crushBox(strings.TrimRight(b.String(), "\n"), width)
}

func crushBox(content string, width int) string {
	w := width - 4
	if w > 62 {
		w = 62
	}
	if w < 30 {
		w = 30
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.AccentColor()).
		Padding(0, 1).
		Width(w).
		Render(content)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
