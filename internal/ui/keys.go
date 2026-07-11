package ui

import "github.com/charmbracelet/bubbles/key"

// keyMap defines the application's global key bindings — those that work from
// any view (view switching, the command prompt, help, refresh, quit). Each view
// contributes its own bindings on top; the shell assembles the two sets when
// rendering help (see help bindings in app.go).
type keyMap struct {
	Dashboard key.Binding
	OSDs      key.Binding
	Pools     key.Binding
	Crush     key.Binding
	Flags     key.Binding
	Services  key.Binding
	PGs       key.Binding
	Command   key.Binding
	Help      key.Binding
	Quit      key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Dashboard: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "dashboard"),
		),
		OSDs: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "osds"),
		),
		Pools: key.NewBinding(
			key.WithKeys("3"),
			key.WithHelp("3", "pools"),
		),
		Crush: key.NewBinding(
			key.WithKeys("4"),
			key.WithHelp("4", "crush"),
		),
		Flags: key.NewBinding(
			key.WithKeys("5"),
			key.WithHelp("5", "flags"),
		),
		Services: key.NewBinding(
			key.WithKeys("6"),
			key.WithHelp("6", "services"),
		),
		PGs: key.NewBinding(
			key.WithKeys("7"),
			key.WithHelp("7", "pgs"),
		),
		Command: key.NewBinding(
			key.WithKeys(":"),
			key.WithHelp(":", "command"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}
