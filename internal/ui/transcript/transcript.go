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
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	vp       viewport.Model
	messages []session.Message
	focused  bool
	width    int
	height   int
	ready    bool
}

func New(msgs []session.Message) Model {
	return Model{messages: msgs}
}

func (m *Model) SetMessages(msgs []session.Message) {
	m.messages = msgs
	if m.ready {
		atBottom := m.vp.AtBottom()
		m.vp.SetContent(m.render())
		// Follow new output only if the user was already at the bottom;
		// yanking them down mid-scroll would lose their place.
		if atBottom {
			m.vp.GotoBottom()
		}
	}
}

// chromeRows is the height the frame takes that is not transcript: two border
// rows, the title and the blank line under it.
const chromeRows = 4

func (m *Model) SetSize(w, h int) {
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
	m.vp.SetContent(m.render())
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
	if !m.focused || !m.ready {
		return m, nil
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// render builds the full transcript text, inserting a divider wherever the
// producing provider or model changes between consecutive assistant turns.
func (m Model) render() string {
	if len(m.messages) == 0 {
		return styles.Ghost.Render("no messages yet")
	}

	var b strings.Builder
	lastProvider, lastModel := "", ""

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
	return b.String()
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
