package components

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cinpol/argonaut/internal/ui/styles"
)

// FormState reports where a Form is in its lifecycle.
type FormState int

const (
	FormActive FormState = iota
	FormSubmitted
	FormCancelled
)

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldChoice
)

// Field is one row of a Form: either a free-text input or a fixed set of
// choices. Construct with TextField or ChoiceField.
type Field struct {
	Label   string
	kind    fieldKind
	input   textinput.Model
	choices []string
	choice  int
}

// TextField creates a free-text field with an optional initial value.
func TextField(label, placeholder, value string) Field {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(value)
	ti.CharLimit = 64
	return Field{Label: label, kind: fieldText, input: ti}
}

// ChoiceField creates a single-select field cycled with ←/→.
func ChoiceField(label string, choices []string, selected int) Field {
	return Field{Label: label, kind: fieldChoice, choices: choices, choice: selected}
}

// Value returns the field's current value (trimmed text, or selected choice).
func (f Field) Value() string {
	if f.kind == fieldChoice {
		if f.choice >= 0 && f.choice < len(f.choices) {
			return f.choices[f.choice]
		}
		return ""
	}
	return strings.TrimSpace(f.input.Value())
}

// Form is a reusable, keyboard-driven multi-field form. It is domain-agnostic;
// callers read field values after FormSubmitted and build the appropriate
// operation. Tab/↑/↓ move between fields, ←/→ cycle a choice, Enter submits,
// Esc cancels.
type Form struct {
	title  string
	fields []Field
	focus  int
	state  FormState
}

// NewForm builds an active form from the given fields.
func NewForm(title string, fields ...Field) Form {
	f := Form{title: title, fields: fields}
	f.focusField(0)
	return f
}

func (f *Form) focusField(i int) {
	for j := range f.fields {
		if f.fields[j].kind == fieldText {
			if j == i {
				f.fields[j].input.Focus()
			} else {
				f.fields[j].input.Blur()
			}
		}
	}
	f.focus = i
}

func (f Form) Update(msg tea.Msg) (Form, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		if f.focused().kind == fieldText {
			var cmd tea.Cmd
			f.fields[f.focus].input, cmd = f.fields[f.focus].input.Update(msg)
			return f, cmd
		}
		return f, nil
	}

	switch km.String() {
	case "esc":
		f.state = FormCancelled
		return f, nil
	case "enter":
		f.state = FormSubmitted
		return f, nil
	case "tab", "down":
		f.focusField((f.focus + 1) % len(f.fields))
		return f, nil
	case "shift+tab", "up":
		f.focusField((f.focus - 1 + len(f.fields)) % len(f.fields))
		return f, nil
	}

	if f.focused().kind == fieldChoice {
		switch km.String() {
		case "left":
			f.cycleChoice(-1)
		case "right", " ":
			f.cycleChoice(1)
		}
		return f, nil
	}

	var cmd tea.Cmd
	f.fields[f.focus].input, cmd = f.fields[f.focus].input.Update(km)
	return f, cmd
}

func (f Form) focused() Field { return f.fields[f.focus] }

func (f *Form) cycleChoice(d int) {
	fl := &f.fields[f.focus]
	if n := len(fl.choices); n > 0 {
		fl.choice = (fl.choice + d + n) % n
	}
}

// State reports the form's lifecycle state.
func (f Form) State() FormState { return f.state }

// Reactivate returns the form to the active state, e.g. after a caller rejects a
// submission because validation failed, so the operator can keep editing.
func (f Form) Reactivate() Form {
	f.state = FormActive
	return f
}

// Value returns field i's current value.
func (f Form) Value(i int) string { return f.fields[i].Value() }

// View renders the form box at the given available width.
func (f Form) View(width int) string {
	w := clampWidth(width-4, 40, 70)

	var b strings.Builder
	b.WriteString(styles.PanelTitle.Render(f.title) + "\n\n")
	for i, fl := range f.fields {
		pointer := "  "
		labelStyle := styles.Faint
		if i == f.focus {
			pointer = styles.Accent.Render("▸ ")
			labelStyle = styles.Label
		}
		b.WriteString(pointer + labelStyle.Render(padRight(fl.Label, 12)) + " ")
		if fl.kind == fieldChoice {
			b.WriteString(renderChoices(fl))
		} else {
			b.WriteString(fl.input.View())
		}
		b.WriteString("\n")
	}
	b.WriteString("\n" + styles.Faint.Render("[tab] next   [↑/↓] move   [←/→] choose   [enter] submit   [esc] cancel"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.AccentColor()).
		Padding(0, 1).
		Width(w)
	return box.Render(b.String())
}

func renderChoices(f Field) string {
	parts := make([]string, len(f.choices))
	for i, c := range f.choices {
		if i == f.choice {
			parts[i] = styles.Accent.Render("(•) " + c)
		} else {
			parts[i] = styles.Faint.Render("( ) " + c)
		}
	}
	return strings.Join(parts, "  ")
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
