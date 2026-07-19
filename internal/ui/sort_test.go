package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cinpol/siphon/internal/ceph/mock"
	"github.com/cinpol/siphon/internal/model"
	"github.com/cinpol/siphon/internal/service"
)

// shiftKey builds the KeyMsg produced by Shift+<letter> (an uppercase rune).
func shiftKey(letter string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(letter)}
}

func newIntSort() tableSort[int] {
	return newTableSort(
		sortKey[int]{"VAL", "V", func(a, b int) bool { return a < b }},
	)
}

func TestTableSortNaturalOrderUntouched(t *testing.T) {
	s := newIntSort()
	items := []int{3, 1, 2}
	s.apply(items) // nothing selected → leave as-is
	if items[0] != 3 || items[1] != 1 || items[2] != 2 {
		t.Fatalf("natural order was modified: %v", items)
	}
}

func TestTableSortSelectAndToggle(t *testing.T) {
	s := newIntSort()

	if !s.handleKey(shiftKey("V")) {
		t.Fatal("Shift+V should select the VAL column")
	}
	items := []int{3, 1, 2}
	s.apply(items)
	if items[0] != 1 || items[1] != 2 || items[2] != 3 {
		t.Fatalf("ascending sort wrong: %v", items)
	}

	// Pressing the same key again toggles to descending.
	if !s.handleKey(shiftKey("V")) {
		t.Fatal("Shift+V again should toggle direction")
	}
	s.apply(items)
	if items[0] != 3 || items[1] != 2 || items[2] != 1 {
		t.Fatalf("descending sort wrong: %v", items)
	}
}

func TestTableSortUnknownKeyIgnored(t *testing.T) {
	s := newIntSort()
	if s.handleKey(shiftKey("Z")) {
		t.Fatal("an unmapped key must not be treated as a sort selection")
	}
	if s.handleKey(shiftKey("v")) {
		t.Fatal("lowercase (unshifted) key must not match")
	}
}

func TestTableSortStableForEqualKeys(t *testing.T) {
	// Equal comparator keys must preserve input order (stable sort), so a
	// secondary visual grouping the caller set up isn't scrambled.
	type pair struct{ k, seq int }
	s := newTableSort(sortKey[pair]{"K", "K", func(a, b pair) bool { return a.k < b.k }})
	s.handleKey(shiftKey("K"))
	items := []pair{{1, 0}, {1, 1}, {1, 2}}
	s.apply(items)
	for i, p := range items {
		if p.seq != i {
			t.Fatalf("stable order broken at %d: %+v", i, items)
		}
	}
}

func TestTableSortDecorate(t *testing.T) {
	s := newTableSort(sortKey[int]{"PG_NUM", "P", func(a, b int) bool { return a < b }})
	cols := []table.Column{{Title: "NAME", Width: 10}, {Title: "PG_NUM", Width: 7}}

	// Nothing selected: columns returned unchanged (but a distinct slice).
	if d := s.decorate(cols); d[1].Title != "PG_NUM" || d[1].Width != 7 {
		t.Fatalf("unsorted decorate should be a no-op, got %+v", d[1])
	}

	s.handleKey(shiftKey("P")) // ascending
	d := s.decorate(cols)
	if d[1].Title != "PG_NUM ↑" {
		t.Fatalf("expected ascending arrow, got %q", d[1].Title)
	}
	if d[1].Width != 9 {
		t.Fatalf("expected width reserved for arrow (+2), got %d", d[1].Width)
	}
	if cols[1].Title != "PG_NUM" || cols[1].Width != 7 {
		t.Fatalf("decorate must not mutate its input: %+v", cols[1])
	}

	s.handleKey(shiftKey("P")) // toggle to descending
	if d := s.decorate(cols); d[1].Title != "PG_NUM ↓" {
		t.Fatalf("expected descending arrow, got %q", d[1].Title)
	}
}

func TestTableSortHint(t *testing.T) {
	s := newTableSort(
		sortKey[int]{"NAME", "N", func(a, b int) bool { return a < b }},
		sortKey[int]{"PG_NUM", "P", func(a, b int) bool { return a < b }},
	)
	if h := s.hint(); h.Key != "⇧N/P" || h.Label != "Sort" {
		t.Fatalf("inactive hint wrong: %+v", h)
	}
	s.handleKey(shiftKey("P"))
	s.handleKey(shiftKey("P")) // descending
	if h := s.hint(); h.Label != "Sort: pg_num ↓" {
		t.Fatalf("active hint wrong: %q", h.Label)
	}
}

// TestPoolViewSortByPGNum drives the real Pools view: Shift+P orders the table
// ascending by pg_num, and pressing it again toggles to descending — exercising
// the sort/filter/selection wiring end to end.
func TestPoolViewSortByPGNum(t *testing.T) {
	pm := newPoolModel(service.New(mock.New()))
	pm.setSize(120, 30)
	pm, _ = pm.Update(pm.fetch()()) // load pools
	if len(pm.pools) < 2 {
		t.Skip("mock has too few pools to exercise sorting")
	}

	pm, _ = pm.Update(shiftKey("P")) // ascending
	for i := 1; i < len(pm.pools); i++ {
		if pm.pools[i-1].PGNum > pm.pools[i].PGNum {
			t.Fatalf("pools not ascending by pg_num after Shift+P: %v", poolPGNums(pm.pools))
		}
	}
	if got := pm.sort.hint().Label; got != "Sort: pg_num ↑" {
		t.Fatalf("expected ascending pg_num hint, got %q", got)
	}

	pm, _ = pm.Update(shiftKey("P")) // toggle → descending
	for i := 1; i < len(pm.pools); i++ {
		if pm.pools[i-1].PGNum < pm.pools[i].PGNum {
			t.Fatalf("pools not descending by pg_num after toggle: %v", poolPGNums(pm.pools))
		}
	}
}

func poolPGNums(pools []model.Pool) []int {
	out := make([]int, len(pools))
	for i, p := range pools {
		out[i] = p.PGNum
	}
	return out
}
