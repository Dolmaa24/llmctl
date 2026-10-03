// Package confirm asks the user to confirm a destructive action before the
// root model performs it (SRS NFR-8).
//
// Only "y" confirms. Enter does not: it is the key people press to get
// through every other dialog, and a destructive action should take a
// deliberate keystroke rather than a reflexive one.
package confirm

import (
	"github.com/Dolmaa24/llmctl/internal/ui/styles"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ConfirmedMsg and CancelledMsg report the user's decision. The dialog
// performs no side effects; the root model acts on the decision.
type (
	ConfirmedMsg struct{}
	CancelledMsg struct{}
)

var keys = struct {
	Confirm, Cancel key.Binding
}{
	Confirm: key.NewBinding(key.WithKeys("y")),
	Cancel:  key.NewBinding(key.WithKeys("esc", "n", "q")),
}

const maxWidth = 60

type Model struct {
	title   string
	body    string
	action  string // what confirming does, shown on the confirm key: "remove"
	visible bool
	width   int
	height  int
}

func New() Model { return Model{} }

// Show opens the dialog. action names what confirming does, so the
// consequence is written on the key itself rather than behind a bare "yes".
func (m *Model) Show(title, body, action string) {
	m.title, m.body, m.action, m.visible = title, body, action, true
}

func (m *Model) Hide()        { m.visible = false }
func (m Model) Visible() bool { return m.visible }

func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch {
	case key.Matches(km, keys.Confirm):
		m.visible = false
		return m, func() tea.Msg { return ConfirmedMsg{} }
	case key.Matches(km, keys.Cancel):
		m.visible = false
		return m, func() tea.Msg { return CancelledMsg{} }
	}
	return m, nil
}

func (m Model) dialogWidth() int {
	w := m.width - 4
	if w > maxWidth {
		w = maxWidth
	}
	if w < 40 {
		w = 40
	}
	return w
}

func (m Model) View() string {
	if !m.visible {
		return ""
	}
	footer := styles.Key.Render("y") + styles.Help.Render(" "+m.action+"   ") +
		styles.Key.Render("esc") + styles.Help.Render(" cancel")
	content := styles.Title.Render(m.title) + "\n\n" + styles.Body.Render(m.body) + "\n\n" + footer

	w := m.dialogWidth()
	body := lipgloss.NewStyle().Width(w - 8).Render(content)
	dialog := styles.Modal.Width(w - 2).Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}
