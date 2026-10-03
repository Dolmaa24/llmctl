// Package composer is where the user writes a message.
//
// Enter sends and Ctrl+J inserts a newline, because most messages to a model
// are one line and should not need a modifier to send. Alt+Enter inserts a
// newline too, but Windows Terminal takes it for full screen by default, so
// Ctrl+J is the key the help bar shows. Pasted text keeps
// its newlines regardless: Bubble Tea delivers a bracketed paste as a single
// message rather than as a series of Enter presses, so a pasted stack trace
// arrives whole instead of being sent line by line.
package composer

import (
	"fmt"
	"strings"

	"github.com/Dolmaa24/llmctl/internal/ui/styles"
	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SubmitMsg carries a message the user has asked to send. The composer only
// reports the intent; the root model decides what sending means.
type SubmitMsg struct{ Text string }

var submit = key.NewBinding(key.WithKeys("enter"))

// Rows is the composer's fixed height: two border rows, the title, and three
// lines of text. It is fixed rather than growing with the text so that typing
// a long message never pushes the rest of the layout around.
const Rows = 6

const textRows = 3

type Model struct {
	ta      textarea.Model
	focused bool
	busy    bool
	target  string // "provider: model", shown so the user knows who they are addressing
	width   int
	height  int
}

func New() Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.Placeholder = "Write a message"
	ta.SetHeight(textRows)

	// Enter is bound to InsertNewline by default. Left alone, one keypress
	// would both insert a newline and submit.
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j", "alt+enter"))

	// A blinking cursor re-arms a timer on every blink for as long as the
	// program runs. Static is calmer and wakes the program only on input.
	ta.Cursor.SetMode(cursor.CursorStatic)

	// The default style highlights the cursor line, which reads as a
	// selection inside a bordered pane.
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = styles.Ghost
	ta.BlurredStyle.Placeholder = styles.Ghost
	ta.FocusedStyle.Text = styles.Body
	ta.BlurredStyle.Text = styles.Dim

	return Model{ta: ta}
}

// SetSize sets the outer size. Height is normally Rows; it is accepted as a
// parameter so a very short terminal can give the composer less.
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.ta.SetWidth(styles.Inner(w))
	rows := h - 3 // border and title
	if rows < 1 {
		rows = 1
	}
	if rows > textRows {
		rows = textRows
	}
	m.ta.SetHeight(rows)
}

// SetTarget records which provider and model a message will go to.
func (m *Model) SetTarget(provider, model string) {
	m.target = fmt.Sprintf("%s: %s", provider, model)
}

// SetBusy marks a request as in flight. The user can keep typing their next
// message, but Enter will not send it until the reply has arrived.
func (m *Model) SetBusy(busy bool) { m.busy = busy }

func (m *Model) Focus() tea.Cmd {
	m.focused = true
	return m.ta.Focus()
}

func (m *Model) Blur() {
	m.focused = false
	m.ta.Blur()
}

func (m Model) Focused() bool { return m.focused }
func (m Model) Value() string { return m.ta.Value() }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok && key.Matches(km, submit) {
		text := strings.TrimSpace(m.ta.Value())
		if text == "" || m.busy {
			return m, nil
		}
		m.ta.Reset()
		return m, func() tea.Msg { return SubmitMsg{Text: text} }
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	title := styles.PaneTitle("MESSAGE", m.focused)
	switch {
	case m.busy:
		title += styles.Ghost.Render("  waiting for reply · esc to cancel")
	case m.target != "":
		title += styles.Dim.Render("  → " + m.target)
	}
	return styles.Frame(title+"\n"+m.ta.View(), m.width, m.height, m.focused)
}
