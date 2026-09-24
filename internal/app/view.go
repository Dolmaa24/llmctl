package app

import (
	"github.com/Dolmaa24/llmctl/internal/ui/composer"
	"github.com/Dolmaa24/llmctl/internal/ui/styles"
	"github.com/Dolmaa24/llmctl/internal/ui/switchconfirm"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Column proportions. The transcript is the reading surface and gets the
// remainder; the two sidebars are sized to their content.
const (
	providerWidth = 28
	notesWidth    = 34
	statusHeight  = 1
	helpHeight    = 1

	// minTranscript is the narrowest the reading surface may get before a
	// sidebar is dropped to make room.
	minTranscript = 40

	// composerMinRows is the smallest composer that still shows one line of
	// text, used when the terminal is too short for the full one.
	composerMinRows = 4
)

// layout sizes every pane. It returns a command only when focus has to move
// because the focused pane no longer fits.
func (m *Model) layout() tea.Cmd {
	bodyHeight := m.height - statusHeight - helpHeight
	if bodyHeight < 6 {
		bodyHeight = 6
	}

	// On a narrow terminal the sidebars would squeeze the transcript to
	// nothing, so give up the notes pane first, then the provider list.
	m.showNotes = m.width >= providerWidth+notesWidth+minTranscript
	m.showProviders = m.width >= providerWidth+minTranscript

	pw, nw := 0, 0
	if m.showProviders {
		pw = providerWidth
	}
	if m.showNotes {
		nw = notesWidth
	}
	tw := m.width - pw - nw

	// The composer sits under the transcript in the middle column. On a short
	// terminal it gives up text rows before the transcript gives up its own.
	cr := composer.Rows
	if bodyHeight-cr < 6 {
		cr = composerMinRows
	}

	m.providers.SetSize(pw, bodyHeight)
	m.transcript.SetSize(tw, bodyHeight-cr)
	m.composer.SetSize(tw, cr)
	m.notes.SetSize(nw, bodyHeight)
	m.status.SetWidth(m.width)
	m.confirm.SetSize(m.width, m.height)
	m.form.SetSize(m.width, m.height)

	if !m.visible(m.focus) {
		return m.setFocus(paneComposer)
	}
	return nil
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		return "starting…"
	}
	// Overlays replace the screen while open rather than being laid out
	// beside it, so the panes underneath never reflow.
	if m.form.Visible() {
		return m.form.View()
	}
	if m.confirm.Visible() {
		return m.confirm.View()
	}

	middle := lipgloss.JoinVertical(lipgloss.Left, m.transcript.View(), m.composer.View())

	var cols []string
	if m.showProviders {
		cols = append(cols, m.providers.View())
	}
	cols = append(cols, middle)
	if m.showNotes {
		cols = append(cols, m.notes.View())
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	return lipgloss.JoinVertical(lipgloss.Left, body, m.status.View(), m.help())
}

// help shows the keys that do something in the current context. A static line
// would advertise "q quit" while the composer is focused, where q types a q.
func (m Model) help() string {
	var pairs [][2]string
	switch {
	case m.focus == paneComposer && m.inFlight:
		pairs = [][2]string{{"esc", "cancel"}, {"tab", "pane"}, {"ctrl+c", "quit"}}
	case m.focus == paneComposer:
		pairs = [][2]string{{"enter", "send"}, {"alt+enter", "newline"}, {"tab", "pane"}, {"ctrl+c", "quit"}}
	case m.focus == paneProviders:
		pairs = [][2]string{{"↑↓", "move"}, {"s", "switch"}, {"a", "add"}, {"e", "edit"}, {"tab", "pane"}, {"q", "quit"}}
	case m.focus == paneNotes:
		pairs = [][2]string{{"↑↓", "scroll"}, {"t", "filter"}, {"tab", "pane"}, {"q", "quit"}}
	default:
		pairs = [][2]string{{"↑↓", "scroll"}, {"tab", "pane"}, {"q", "quit"}}
	}

	var out string
	for i, p := range pairs {
		if i > 0 {
			out += styles.Help.Render("   ")
		}
		out += styles.Key.Render(p[0]) + styles.Help.Render(" "+p[1])
	}
	return lipgloss.NewStyle().Width(m.width).MaxHeight(1).Padding(0, 1).Render(out)
}

// planFor builds the comparison shown before a switch. Integration replaces
// this with session.HandoffBuilder and costestimate.Estimator; the modal's
// input shape does not change when it does.
func (m Model) planFor(targetID string) switchconfirm.Plan {
	from, _ := m.activeProvider()
	full, distilled, extraction, notes, raw := demoPlanNumbers()
	return switchconfirm.Plan{
		FromProvider:     from.ID,
		FromModel:        from.Model,
		ToProvider:       targetID,
		ToModel:          m.modelFor(targetID),
		NotesCount:       notes,
		RawTurnCount:     raw,
		TokensFullReplay: full,
		TokensDistilled:  distilled,
		TokensExtraction: extraction,
	}
}
