package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// reset clears any active SGR styling. It is inserted around the popup so the
// background's colours can't bleed into it and vice versa.
const reset = "\x1b[0m"

// overlayCenter composites a foreground box (a popup: details, form or
// confirmation) centred on top of a background (the page the user came from),
// keeping the background visible around the popup so context is never lost.
//
// Both inputs are treated as styled (ANSI) text. The background is normalised to
// exactly width×height; the popup is drawn opaque over the centre, and the
// surrounding background rows/columns show through.
func overlayCenter(background, popup string, width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	bg := lipgloss.NewStyle().
		Width(width).MaxWidth(width).
		Height(height).MaxHeight(height).
		Render(background)
	bgLines := strings.Split(bg, "\n")

	fgLines := strings.Split(popup, "\n")
	fgW := 0
	for _, l := range fgLines {
		if w := ansi.StringWidth(l); w > fgW {
			fgW = w
		}
	}
	if fgW > width {
		fgW = width
	}

	top := (height - len(fgLines)) / 2
	if top < 0 {
		top = 0
	}
	left := (width - fgW) / 2
	if left < 0 {
		left = 0
	}

	for i, fl := range fgLines {
		row := top + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		bgLine := bgLines[row]

		leftPart := ansi.Truncate(bgLine, left, "")
		if pad := left - ansi.StringWidth(leftPart); pad > 0 {
			leftPart += strings.Repeat(" ", pad)
		}
		rightPart := ansi.TruncateLeft(bgLine, left+fgW, "")

		// Pad the popup line so its box stays opaque over the background.
		if pad := fgW - ansi.StringWidth(fl); pad > 0 {
			fl += strings.Repeat(" ", pad)
		}

		bgLines[row] = leftPart + reset + fl + reset + rightPart
	}
	return strings.Join(bgLines, "\n")
}
