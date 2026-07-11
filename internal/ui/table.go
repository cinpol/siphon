package ui

import "github.com/charmbracelet/bubbles/table"

// fitColumns scales a table's base column widths proportionally so the table
// fills the available width (the workspace panel's interior). Every column grows
// in proportion to its natural width — the redesign's chosen behaviour — with
// any rounding remainder handed to the widest column.
//
// bubbles' table renders each cell with one space of padding on each side, so
// the per-column overhead (2×N) is subtracted before distributing, and a small
// safety margin is left so the table never overflows the panel border.
func fitColumns(cols []table.Column, width int) []table.Column {
	if len(cols) == 0 || width <= 0 {
		return cols
	}

	base := 0
	for _, c := range cols {
		base += c.Width
	}
	if base <= 0 {
		return cols
	}

	avail := width - 2*len(cols) - 1
	if avail <= base {
		return cols // too narrow to grow; keep natural widths
	}

	out := make([]table.Column, len(cols))
	used := 0
	widest := 0
	for i, c := range cols {
		w := c.Width * avail / base
		out[i] = table.Column{Title: c.Title, Width: w}
		used += w
		if c.Width > cols[widest].Width {
			widest = i
		}
	}
	if leftover := avail - used; leftover > 0 {
		out[widest].Width += leftover
	}
	return out
}
