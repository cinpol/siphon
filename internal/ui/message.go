package ui

import "github.com/cinpol/siphon/internal/ui/styles"

// viewMessage is the one message line at the top of every resource view. It
// holds either a failed operation's error (red) or a notice (grey, e.g. "no
// changes to apply" or why an action isn't possible), and every view treats it
// the same way:
//
//   - a newer message replaces it, and a successful operation clears it;
//   - the operator's next keypress clears it (Model.handleKey → clearMessage),
//     except keys typed into an open form, dialog or filter — so a refusal stays
//     visible while the operator corrects the input;
//   - the periodic refresh never clears it (it used to, which made errors vanish
//     within one refresh interval).
//
// The "refresh failed" banner is separate (staleLine): it reports the state of
// the connection, so it goes away when a refresh succeeds.
type viewMessage struct {
	text    string
	isError bool
}

func (m *viewMessage) setError(err error) {
	if err == nil {
		m.clear()
		return
	}
	*m = viewMessage{text: err.Error(), isError: true}
}

func (m *viewMessage) setNotice(text string) { *m = viewMessage{text: text} }
func (m *viewMessage) clear()                { *m = viewMessage{} }
func (m viewMessage) failed() bool           { return m.isError && m.text != "" }

// banner renders the message line (plus spacing), or "" when there is none.
func (m viewMessage) banner() string {
	switch {
	case m.text == "":
		return ""
	case m.isError:
		return styles.Danger.Render("operation failed: "+m.text) + "\n\n"
	default:
		return styles.Faint.Render(m.text) + "\n\n"
	}
}

// staleLine renders the "refresh failed — showing last data" banner (plus
// spacing) when the last refresh failed, or "".
func staleLine(err error) string {
	if err == nil {
		return ""
	}
	return staleBanner(err) + "\n\n"
}

// clearMessage implements resourceView for every view.
func (m *osdModel) clearMessage()     { m.msg.clear() }
func (m *poolModel) clearMessage()    { m.msg.clear() }
func (m *crushModel) clearMessage()   { m.msg.clear() }
func (m *flagModel) clearMessage()    { m.msg.clear() }
func (m *serviceModel) clearMessage() { m.msg.clear() }
func (m *pgModel) clearMessage()      { m.msg.clear() }
