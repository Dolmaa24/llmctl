package app

import (
	"github.com/Dolmaa24/llmctl/internal/ui/styles"
	"github.com/Dolmaa24/llmctl/internal/ui/switchconfirm"
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
)

func (m *Model) layout() {
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

	m.providers.SetSize(pw, bodyHeight)
	m.transcript.SetSize(tw, bodyHeight)
	m.notes.SetSize(nw, bodyHeight)
	m.status.SetWidth(m.width)
	m.confirm.SetSize(m.width, m.height)
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 {
		return "starting…"
	}

	var cols []string
	if m.showProviders {
		cols = append(cols, m.providers.View())
	}
	cols = append(cols, m.transcript.View())
	if m.showNotes {
		cols = append(cols, m.notes.View())
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	screen := lipgloss.JoinVertical(lipgloss.Left, body, m.status.View(), m.help())

	// The modal is drawn over the composed screen rather than beside it, so
	// the layout underneath never reflows when it opens.
	if m.confirm.Visible() {
		return m.confirm.View()
	}
	return screen
}

func (m Model) help() string {
	pairs := [][2]string{
		{"tab", "pane"},
		{"↑↓", "move"},
		{"s", "switch"},
		{"q", "quit"},
	}
	var out string
	for i, p := range pairs {
		if i > 0 {
			out += styles.Help.Render("   ")
		}
		out += styles.Key.Render(p[0]) + styles.Help.Render(" "+p[1])
	}
	return lipgloss.NewStyle().Width(m.width).Padding(0, 1).Render(out)
}

// planFor builds the comparison shown before a switch. Phase 2 replaces this
// with session.HandoffBuilder and costestimate.Estimator; the modal's input
// shape does not change when it does.
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
