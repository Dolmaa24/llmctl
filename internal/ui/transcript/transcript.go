// Package transcript renders the scrollable, attributed conversation history.
//
// Attribution is the point of this pane, not decoration: PRD Feature 3
// requires that the user can always see which model produced which part of the
// conversation, and that a mid-session switch is visible as a divider.
package transcript

import (
	"fmt"
	"strings"

	"github.com/Dolmaa24/llmctl/internal/session"
	"github.com/Dolmaa24/llmctl/internal/ui/styles"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// chromeRows is the height the frame takes that is not transcript: two border
// rows, the title and the blank line under it.
const chromeRows = 4

type Model struct {
	vp       viewport.Model
	spin     spinner.Model
	messages []session.Message

	// body caches the rendered messages. Wrapping every message is the
	// expensive part of drawing this pane, and the spinner redraws about ten
	// times a second while a reply is pending; without the cache each frame
	// would re-wrap the whole conversation. It is rebuilt only when the
	// messages or the width change.
	body string

	// The provider and model of the last assistant turn in body, so a pending
	// reply from a different model can draw its divider before it arrives.
	lastProvider, lastModel string

	pending                     bool
	pendingProvider, pendingMod string

	// notice is a transient line such as a provider error. It is shown in the
	// transcript but is not a message: it must never be sent to a model as
	// part of the conversation history. noticeOK marks it as a success, such
	// as a finished export, rather than a problem.
	notice   string
	noticeOK bool

	focused bool
	width   int
	height  int
	ready   bool
}

func New(msgs []session.Message) Model {
	return Model{
		messages: msgs,
		spin: spinner.New(
			spinner.WithSpinner(spinner.MiniDot),
			spinner.WithStyle(styles.Attribution),
		),
	}
}

func (m *Model) SetMessages(msgs []session.Message) {
	m.messages = msgs
	if !m.ready {
		return
	}
	atBottom := m.vp.AtBottom()
	m.rebuild()
	// Follow new output only if the user was already at the bottom; yanking
	// them down mid-scroll would lose their place.
	if atBottom {
		m.vp.GotoBottom()
	}
}

// ScrollToBottom jumps to the latest turn. Used when the user sends a message:
// having just acted, they want to see the result, wherever they had scrolled.
func (m *Model) ScrollToBottom() {
	if m.ready {
		m.vp.GotoBottom()
	}
}

// SetPending shows that a reply from provider/model is on its way, and returns
// the command that starts the spinner.
func (m *Model) SetPending(provider, model string) tea.Cmd {
	m.pending = true
	m.pendingProvider, m.pendingMod = provider, model
	m.notice = ""
	if m.ready {
		m.refresh()
		m.vp.GotoBottom()
	}
	return m.spin.Tick
}

func (m *Model) ClearPending() {
	m.pending = false
	if m.ready {
		m.refresh()
	}
}

// SetNotice shows a transient line at the foot of the transcript, styled as a
// problem.
func (m *Model) SetNotice(s string) { m.showNotice(s, false) }

// SetSuccess shows a transient line reporting that something worked. It must
// not look like an error, or the user will doubt an export that succeeded.
func (m *Model) SetSuccess(s string) { m.showNotice(s, true) }

func (m *Model) showNotice(s string, ok bool) {
	m.notice, m.noticeOK = s, ok
	if m.ready {
		m.refresh()
		m.vp.GotoBottom()
	}
}

func (m Model) Pending() bool  { return m.pending }
func (m Model) Notice() string { return m.notice }

func (m *Model) SetSize(w, h int) {
	widthChanged := w != m.width
	m.width, m.height = w, h
	iw, ih := styles.Inner(w), h-chromeRows
	if ih < 1 {
		ih = 1
	}
	first := !m.ready
	if first {
		m.vp = viewport.New(iw, ih)
		m.ready = true
	} else {
		m.vp.Width, m.vp.Height = iw, ih
	}
	if first || widthChanged {
		m.rebuild()
	} else {
		m.refresh()
	}
	// A conversation opens on its latest turn. The viewport defaults to the
	// top, which would greet the user with the oldest message and hide the
	// newest one below the fold.
	if first {
		m.vp.GotoBottom()
	}
}

func (m *Model) Focus()       { m.focused = true }
func (m *Model) Blur()        { m.focused = false }
func (m Model) Focused() bool { return m.focused }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	// Spinner ticks arrive whether or not this pane has focus: the user is
	// usually typing in the composer while they wait.
	if tick, ok := msg.(spinner.TickMsg); ok {
		if !m.pending {
			return m, nil // returning no command lets the spinner stop
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(tick)
		if m.ready {
			m.refresh()
		}
		return m, cmd
	}
	if !m.focused || !m.ready {
		return m, nil
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// rebuild re-renders the cached body, then refreshes the viewport.
func (m *Model) rebuild() {
	m.body, m.lastProvider, m.lastModel = m.renderMessages()
	m.refresh()
}

// refresh sets the viewport content from the cached body plus the pending or
// notice line. It is cheap, and is what spinner ticks call.
func (m *Model) refresh() {
	m.vp.SetContent(m.body + m.tail())
}

// renderMessages renders every message, inserting a divider wherever the
// producing provider or model changes between consecutive assistant turns.
func (m Model) renderMessages() (body, lastProvider, lastModel string) {
	if len(m.messages) == 0 {
		return styles.Ghost.Render("no messages yet"), "", ""
	}

	var b strings.Builder
	for i, msg := range m.messages {
		if msg.Provider != "" && (msg.Provider != lastProvider || msg.Model != lastModel) {
			if lastProvider != "" {
				b.WriteString("\n" + m.divider(msg.Provider, msg.Model) + "\n")
			}
			lastProvider, lastModel = msg.Provider, msg.Model
		}
		b.WriteString(m.renderMessage(msg))
		if i < len(m.messages)-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String(), lastProvider, lastModel
}

// tail renders what follows the messages: the pending reply or a notice.
func (m Model) tail() string {
	switch {
	case m.pending:
		var b strings.Builder
		b.WriteString("\n\n")
		// Draw the divider now, not when the reply lands, so the transcript
		// does not jump a line under the user when it arrives.
		if m.lastProvider != "" &&
			(m.pendingProvider != m.lastProvider || m.pendingMod != m.lastModel) {
			b.WriteString(m.divider(m.pendingProvider, m.pendingMod) + "\n")
		}
		b.WriteString(styles.Attribution.Render(fmt.Sprintf("%s: %s", m.pendingProvider, m.pendingMod)))
		b.WriteString("\n" + m.spin.View() + styles.Dim.Render(" thinking…"))
		return b.String()

	case m.notice != "":
		// The symbol marks success or failure by shape as well as colour.
		colour, symbol := styles.Fail, "! "
		if m.noticeOK {
			colour, symbol = styles.OK, "✓ "
		}
		return "\n\n" + lipgloss.NewStyle().
			Width(styles.Inner(m.width)).
			Foreground(colour).
			Render(symbol+m.notice)
	}
	return ""
}

func (m Model) renderMessage(msg session.Message) string {
	var head string
	switch msg.Role {
	case session.RoleUser:
		head = styles.Dim.Render("you")
	case session.RoleSystem:
		head = styles.Ghost.Render("system")
	default:
		head = styles.Attribution.Render(fmt.Sprintf("%s: %s", msg.Provider, msg.Model))
	}

	body := lipgloss.NewStyle().
		Width(styles.Inner(m.width)).
		Foreground(styles.Text).
		Render(msg.Content)

	return head + "\n" + body
}

// divider is the visible mark at a provider switch, required by SRS FR-3.6.
func (m Model) divider(provider, model string) string {
	label := fmt.Sprintf(" switched to %s: %s ", provider, model)
	w := styles.Inner(m.width)
	rule := w - lipgloss.Width(label)
	if rule < 4 {
		return styles.Divider.Render(label)
	}
	left := rule / 2
	right := rule - left
	return styles.Divider.Render(strings.Repeat("─", left) + label + strings.Repeat("─", right))
}

func (m Model) View() string {
	title := styles.PaneTitle("TRANSCRIPT", m.focused)
	if !m.ready {
		return styles.Frame(title, m.width, m.height, m.focused)
	}
	if m.vp.TotalLineCount() > m.vp.Height {
		title += styles.Ghost.Render(fmt.Sprintf("  %3.0f%%", m.vp.ScrollPercent()*100))
	}
	return styles.Frame(title+"\n\n"+m.vp.View(), m.width, m.height, m.focused)
}
