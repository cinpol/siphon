package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cinpol/argonaut/internal/ui/styles"
)

// ConfirmState reports where a Confirm dialog is in its lifecycle.
type ConfirmState int

const (
	ConfirmActive ConfirmState = iota
	ConfirmAccepted
	ConfirmCancelled
)

// Confirm is the reusable safety dialog for mutating operations. It always shows
// the equivalent Ceph command and the consequence, then asks for a single y/n
// confirmation — the one consistent confirmation model across the whole tool.
//
// Irreversible operations switch on danger styling (a red border and a ⚠ marker)
// so destructive actions still stand out visually, per the project's safety
// model, without the friction of typing a token.
//
// It is transport- and domain-agnostic: callers supply the text, so the same
// dialog serves OSD, pool, CRUSH, flag and service operations.
type Confirm struct {
	title        string
	command      string
	consequence  string
	irreversible bool
	state        ConfirmState
}

// NewYesNo builds an active y/n confirmation. irreversible switches on the
// danger styling used for destructive operations.
func NewYesNo(title, command, consequence string, irreversible bool) Confirm {
	return Confirm{
		title:        title,
		command:      command,
		consequence:  consequence,
		irreversible: irreversible,
		state:        ConfirmActive,
	}
}

// Update folds a message into the dialog: `y` accepts, `n` or esc cancels.
func (c Confirm) Update(msg tea.Msg) (Confirm, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return c, nil
	}
	switch km.String() {
	case "y", "Y":
		c.state = ConfirmAccepted
	case "n", "N", "esc":
		c.state = ConfirmCancelled
	}
	return c, nil
}

// State reports the dialog's lifecycle state.
func (c Confirm) State() ConfirmState { return c.state }

// View renders the dialog box at the given available width.
func (c Confirm) View(width int) string {
	w := clampWidth(width-4, 34, 62)

	borderColor := styles.AccentColor()
	title := c.title
	if c.irreversible {
		borderColor = styles.DangerColor()
		title = "⚠ " + title
	}

	var b strings.Builder
	b.WriteString(styles.PanelTitle.Render(title) + "\n\n")
	for _, line := range strings.Split(c.command, "\n") {
		b.WriteString(styles.Command.Render("$ "+line) + "\n")
	}
	b.WriteString(styles.Faint.Width(w).Render(c.consequence) + "\n\n")
	b.WriteString(styles.Faint.Render("[y] yes   [n] no"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(w)
	return box.Render(b.String())
}

func clampWidth(w, min, max int) int {
	if w < min {
		return min
	}
	if w > max {
		return max
	}
	return w
}
