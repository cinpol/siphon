package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// Action is one context-sensitive operation a view offers on the current
// selection. It is deliberately presentation-only (key + label + danger): the
// actual behaviour stays in each view's key handler, keyed by the same binding.
//
// A view's actions() is the single source of truth for the header's action
// columns (see layout.go). Actions are triggered only by their keyboard
// shortcut; there is no action menu.
type Action struct {
	Key    string // display key, e.g. "o" or "enter"
	Label  string // short verb, e.g. "Mark Out"
	Danger bool   // destructive
}

// act builds an Action from an existing key.Binding, reusing its help text so
// the header stays consistent with the binding. Callers may override Label after
// (e.g. flags toggling between Enable/Disable).
func act(b key.Binding, danger bool) Action {
	h := b.Help()
	return Action{Key: h.Key, Label: capLabel(h.Desc), Danger: danger}
}

// capLabel upper-cases the first letter of a help description ("out" -> "Out").
func capLabel(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
