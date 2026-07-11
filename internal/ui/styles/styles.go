// Package styles centralises Argonaut's Lipgloss styling.
//
// Keeping colours and text styles in one place gives the TUI a consistent look
// and a single point to evolve into a proper theme (including light/dark and
// user-configurable palettes) later.
package styles

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/cinpol/argonaut/internal/model"
)

// Palette. Kept as named vars so a future theme layer can swap them.
var (
	colorAccent   = lipgloss.Color("#7D56F4") // Argonaut purple
	colorOK       = lipgloss.Color("42")      // green
	colorWarn     = lipgloss.Color("214")     // orange
	colorErr      = lipgloss.Color("196")     // red
	colorMuted    = lipgloss.Color("245")     // grey
	colorBlue     = lipgloss.Color("39")      // action-key blue
	colorBabyBlue = lipgloss.Color("117")     // workspace border
	colorWhite    = lipgloss.Color("231")     // bold values / titles
)

var (
	// Title is the application name / primary heading.
	Title = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	// Accent is the brand colour for neutral emphasis (e.g. neutral bars).
	Accent = lipgloss.NewStyle().Foreground(colorAccent)

	// Command styles an equivalent-CLI preview (e.g. "$ ceph osd out 12").
	Command = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))

	// Danger styles destructive/error text.
	Danger = lipgloss.NewStyle().Bold(true).Foreground(colorErr)

	// Faint is used for secondary/metadata text.
	Faint = lipgloss.NewStyle().Foreground(colorMuted)

	// Label styles left-hand field labels.
	Label = lipgloss.NewStyle().Bold(true)

	// PanelBorder wraps a dashboard panel. Width/height are set per panel.
	PanelBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(0, 1)

	// PanelTitle styles the heading line inside a panel.
	PanelTitle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	// Badge renders an inverse pill (used for the health badge in the header).
	Badge = lipgloss.NewStyle().Bold(true).Padding(0, 1)

	// MockBadge flags a session running against the in-memory mock instead of a
	// live cluster — a warning-orange pill shown in the header's Client field so
	// the operator can never mistake demo data for real cluster state.
	MockBadge = Badge.Background(colorWarn).Foreground(lipgloss.Color("232"))

	// Layout styles for the redesigned shell (separator, header columns,
	// workspace panel). Grouped here so the whole chrome is themeable in one
	// place.

	// Separator is the full-width green rule at the top of the screen.
	Separator = lipgloss.NewStyle().Foreground(colorOK)

	// Brand renders the "Argonaut v0.1.0" wordmark in the header.
	Brand = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	// HeaderKey/HeaderValue style the cluster-info column: orange keys, bold
	// white values.
	HeaderKey   = lipgloss.NewStyle().Foreground(colorWarn)
	HeaderValue = lipgloss.NewStyle().Bold(true).Foreground(colorWhite)

	// NavKey/NavLabel style the navigation columns: purple shortcut keys, grey
	// labels; NavActive marks the current page.
	NavKey    = lipgloss.NewStyle().Foreground(colorAccent)
	NavLabel  = lipgloss.NewStyle().Foreground(colorMuted)
	NavActive = lipgloss.NewStyle().Bold(true).Foreground(colorWhite)

	// ActionKey/ActionLabel style the action columns: blue keys, grey labels.
	ActionKey   = lipgloss.NewStyle().Foreground(colorBlue)
	ActionLabel = lipgloss.NewStyle().Foreground(colorMuted)

	// WorkspaceTitle is the bold white page title embedded in the panel border.
	WorkspaceTitle = lipgloss.NewStyle().Bold(true).Foreground(colorWhite)

	healthOK   = lipgloss.NewStyle().Bold(true).Foreground(colorOK)
	healthWarn = lipgloss.NewStyle().Bold(true).Foreground(colorWarn)
	healthErr  = lipgloss.NewStyle().Bold(true).Foreground(colorErr)
	healthDim  = lipgloss.NewStyle().Bold(true).Foreground(colorMuted)
)

// Health returns the text style for a given health status (or check severity,
// which uses the same HEALTH_* vocabulary).
func Health(status model.HealthStatus) lipgloss.Style {
	switch status {
	case model.HealthOK:
		return healthOK
	case model.HealthWarn:
		return healthWarn
	case model.HealthErr:
		return healthErr
	default:
		return healthDim
	}
}

// HealthBadge returns an inverse pill styled by health status, for the header.
func HealthBadge(status model.HealthStatus) lipgloss.Style {
	color := colorMuted
	switch status {
	case model.HealthOK:
		color = colorOK
	case model.HealthWarn:
		color = colorWarn
	case model.HealthErr:
		color = colorErr
	}
	return Badge.Background(color).Foreground(lipgloss.Color("232"))
}

// AccentColor exposes the brand accent colour for widgets (e.g. table headers)
// that need a raw colour rather than a Style.
func AccentColor() lipgloss.Color { return colorAccent }

// BabyBlueColor is the workspace-panel border colour; WhiteColor is used for
// titles/values. Exposed as raw colours for widgets that need them.
func BabyBlueColor() lipgloss.Color { return colorBabyBlue }
func WhiteColor() lipgloss.Color    { return colorWhite }

// SelectedFg/SelectedBg are the colours for a highlighted table row.
func SelectedFg() lipgloss.Color { return lipgloss.Color("231") }
func SelectedBg() lipgloss.Color { return colorAccent }

// DangerColor exposes the destructive-action colour for widget borders.
func DangerColor() lipgloss.Color { return colorErr }

// Utilization returns a colour appropriate to a 0..1 fullness ratio: green
// until 70%, orange until 85%, red beyond. Used for capacity meters.
func Utilization(ratio float64) lipgloss.Style {
	switch {
	case ratio >= 0.85:
		return healthErr
	case ratio >= 0.70:
		return healthWarn
	default:
		return healthOK
	}
}
