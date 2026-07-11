package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cinpol/argonaut/internal/ui/styles"
)

// tableFilter is the shared "/" live filter used by every flat table view (OSDs,
// Pools, Flags, Services). Pressing "/" opens a prompt that the layout manager
// renders just above the workspace panel; typing filters the table live by
// case-insensitive substring across all columns. Enter commits (the rows stay
// filtered and a compact indicator remains); Esc clears it.
//
// The view keeps ownership of its data slice and its table's cursor. The filter
// only records which displayed row maps to which index in the unfiltered slice
// (idx), so the view can translate the cursor back to the selected item — this
// is what lets filtering work without the view copying or reordering its data.
type tableFilter struct {
	input  textinput.Model
	active bool  // prompt open and owning keystrokes
	total  int   // row count before filtering, for the indicator
	idx    []int // displayed-row index -> index in the unfiltered slice
}

func newTableFilter() tableFilter {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "filter"
	ti.CharLimit = 64
	return tableFilter{input: ti}
}

// open focuses the prompt, keeping any existing query so it can be edited.
func (f *tableFilter) open() tea.Cmd {
	f.active = true
	return f.input.Focus()
}

// commit closes the prompt but keeps the query applied (Enter).
func (f *tableFilter) commit() {
	f.active = false
	f.input.Blur()
}

// clear removes the filter entirely (Esc), returning to the full list.
func (f *tableFilter) clear() {
	f.active = false
	f.input.Blur()
	f.input.SetValue("")
}

// query is the normalised search text (trimmed, lower-cased).
func (f tableFilter) query() string {
	return strings.ToLower(strings.TrimSpace(f.input.Value()))
}

// typing reports whether the prompt is open and owns keystrokes (so the view
// must capture input and the shell must not treat keys as global hotkeys).
func (f tableFilter) typing() bool { return f.active }

// applied reports whether a (possibly committed) query is narrowing the list.
func (f tableFilter) applied() bool { return f.query() != "" }

// visible reports whether the prompt/indicator line should render.
func (f tableFilter) visible() bool { return f.active || f.applied() }

// apply filters allRows by the current query, records the row→source index map,
// and returns the rows to display. Cells are joined so the match spans every
// column ("950" matches the id, "ssd" the class, "ceph-02" the host).
func (f *tableFilter) apply(allRows []table.Row) []table.Row {
	f.total = len(allRows)
	f.idx = f.idx[:0]
	q := f.query()
	if q == "" {
		for i := range allRows {
			f.idx = append(f.idx, i)
		}
		return allRows
	}
	kept := make([]table.Row, 0, len(allRows))
	for i, row := range allRows {
		if strings.Contains(strings.ToLower(strings.Join(row, " ")), q) {
			kept = append(kept, row)
			f.idx = append(f.idx, i)
		}
	}
	return kept
}

// source maps a table cursor position to its index in the unfiltered slice.
func (f tableFilter) source(cursor int) (int, bool) {
	if cursor >= 0 && cursor < len(f.idx) {
		return f.idx[cursor], true
	}
	return 0, false
}

// handleKey processes a keystroke while the prompt is open. Navigation keys move
// the (filtered) table so the operator can pre-select a row while still typing;
// Enter commits; Esc clears; anything else edits the query and re-filters. The
// caller passes a freshly built full row set so the filter always reflects the
// latest data. It reports whether the prompt is still open afterwards.
func (f *tableFilter) handleKey(msg tea.KeyMsg, t *table.Model, allRows []table.Row) (tea.Cmd, bool) {
	switch msg.String() {
	case "enter":
		f.commit()
		return nil, false
	case "esc":
		f.clear()
		t.SetRows(f.apply(allRows))
		return nil, false
	case "up", "down", "pgup", "pgdown", "home", "end":
		nt, cmd := t.Update(msg)
		*t = nt
		return cmd, true
	}
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	t.SetRows(f.apply(allRows))
	// Editing the query can shrink the list below the cursor; keep it in range.
	if c := t.Cursor(); c >= len(f.idx) {
		if len(f.idx) == 0 {
			t.GotoTop()
		} else {
			t.SetCursor(len(f.idx) - 1)
		}
	}
	return cmd, true
}

// prompt renders the filter line shown just above the workspace panel: the live
// input while typing, or a compact "applied" indicator once committed.
func (f tableFilter) prompt() string {
	if f.active {
		return f.input.View()
	}
	return styles.NavKey.Render("/") + f.input.Value() +
		styles.Faint.Render(fmt.Sprintf("   %d/%d · esc to clear", len(f.idx), f.total))
}
