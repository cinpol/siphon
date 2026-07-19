package ui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
)

// sortKey pairs a table column with a comparator and the keystroke that selects
// it. key is an uppercase letter — i.e. Shift+<letter> — following the k9s
// convention of sorting by a column with a shifted key.
type sortKey[T any] struct {
	column string            // must equal the table column's Title
	key    string            // selecting keystroke, e.g. "P" for Shift+P
	less   func(a, b T) bool // ascending comparator
}

// tableSort is the shared column sorter for flat table views — the sorting
// counterpart to tableFilter. It stable-sorts the view's own data slice in
// place, so the filter's row→source mapping and the view's selection keep
// working unchanged: the view re-sorts, then re-filters, then rebuilds its rows.
//
// k9s-style, Shift+<column key> selects a column to sort by; pressing the same
// key again toggles ascending/descending. The active column shows a ↑/↓ arrow
// in its header.
type tableSort[T any] struct {
	keys   []sortKey[T]
	active int  // index into keys; -1 means natural (unsorted) order
	desc   bool // descending when true
}

func newTableSort[T any](keys ...sortKey[T]) tableSort[T] {
	return tableSort[T]{keys: keys, active: -1}
}

// apply stable-sorts items in place by the active column and direction. In
// natural order (nothing selected) it leaves items untouched, preserving the
// order the cluster reported.
func (s tableSort[T]) apply(items []T) {
	if s.active < 0 || s.active >= len(s.keys) {
		return
	}
	less := s.keys[s.active].less
	sort.SliceStable(items, func(i, j int) bool {
		if s.desc {
			return less(items[j], items[i])
		}
		return less(items[i], items[j])
	})
}

// handleKey selects or toggles the sort column from a keystroke. It reports
// whether the sort changed, so the caller knows to re-sort and rebuild rows.
func (s *tableSort[T]) handleKey(msg tea.KeyMsg) bool {
	k := msg.String()
	for i, sk := range s.keys {
		if sk.key == k {
			if s.active == i {
				s.desc = !s.desc
			} else {
				s.active = i
				s.desc = false
			}
			return true
		}
	}
	return false
}

// decorate reserves a 2-cell indicator slot on every sortable column and marks
// the active one with a ↑/↓ arrow. Reserving the slot on *all* sortable columns
// (not just the active one) keeps each column's width constant as the active
// column changes, so the layout never shifts when you sort. Call it on the base
// columns *before* fitColumns so the reserved width is accounted for. The input
// slice is not mutated.
func (s tableSort[T]) decorate(cols []table.Column) []table.Column {
	out := make([]table.Column, len(cols))
	copy(out, cols)

	sortable := make(map[string]bool, len(s.keys))
	for _, k := range s.keys {
		sortable[k.column] = true
	}
	active, arrow := "", ""
	if s.active >= 0 && s.active < len(s.keys) {
		active = s.keys[s.active].column
		arrow = " ↑"
		if s.desc {
			arrow = " ↓"
		}
	}
	for i := range out {
		if !sortable[out[i].Title] {
			continue
		}
		out[i].Width += 2 // reserved indicator slot — always, so nothing shifts
		if out[i].Title == active {
			out[i].Title += arrow
		}
	}
	return out
}

// hint is the action-bar affordance: it lists the sort keys and, when a column
// is active, shows what the table is currently sorted by.
func (s tableSort[T]) hint() Action {
	letters := make([]string, len(s.keys))
	for i, sk := range s.keys {
		letters[i] = sk.key
	}
	label := "Sort"
	if s.active >= 0 && s.active < len(s.keys) {
		dir := "↑"
		if s.desc {
			dir = "↓"
		}
		label = "Sort: " + strings.ToLower(s.keys[s.active].column) + " " + dir
	}
	return Action{Key: "⇧" + strings.Join(letters, "/"), Label: label}
}
