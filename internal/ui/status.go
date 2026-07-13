package ui

import "github.com/cinpol/siphon/internal/ui/styles"

// staleBanner is shown above a view's last-good data when a refresh fails, so a
// transient error doesn't wipe what the operator was reading. Retrying (r) or
// the next automatic tick will attempt to refresh again.
func staleBanner(err error) string {
	return styles.Danger.Render("⚠ refresh failed") +
		styles.Faint.Render(" ("+errShort(err)+") — showing last data · [r] retry")
}

// errorScreen is shown when a view has failed and has no previous data to fall
// back on.
func errorScreen(err error) string {
	return styles.Danger.Render("Error: ") + err.Error() + "\n\n" + styles.Faint.Render("[r] retry")
}

func errShort(err error) string {
	s := err.Error()
	if len(s) > 80 {
		return s[:79] + "…"
	}
	return s
}
