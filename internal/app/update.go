package app

import (
	"github.com/Dolmaa24/llmctl/internal/ui/switchconfirm"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case tea.KeyMsg:
		// The modal is exclusive: while it is open it consumes every key, so a
		// stray Tab cannot move focus behind an overlay the user is answering.
		if m.confirm.Visible() {
			var cmd tea.Cmd
			m.confirm, cmd = m.confirm.Update(msg)
			return m, cmd
		}

		switch {
		case key.Matches(msg, Keys.Quit):
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, Keys.NextPane):
			m.setFocus((m.focus + 1) % paneCount)
			return m, nil
		case key.Matches(msg, Keys.PrevPane):
			m.setFocus((m.focus + paneCount - 1) % paneCount)
			return m, nil
		case key.Matches(msg, Keys.Switch):
			// Only meaningful from the provider list, where a target is selected.
			if m.focus == paneProviders {
				if item, ok := m.providers.Selected(); ok {
					m.confirm.Show(m.planFor(item.ID))
					return m, nil
				}
			}
		}

	case switchconfirm.ConfirmedMsg:
		// Phase 2 performs the real switch here. For now the modal simply closes.
		return m, nil

	case switchconfirm.CancelledMsg:
		return m, nil
	}

	var cmd tea.Cmd
	m.providers, cmd = m.providers.Update(msg)
	cmds = append(cmds, cmd)
	m.transcript, cmd = m.transcript.Update(msg)
	cmds = append(cmds, cmd)
	m.notes, cmd = m.notes.Update(msg)
	cmds = append(cmds, cmd)
	m.confirm, cmd = m.confirm.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) setFocus(p pane) {
	m.focus = p
	m.providers.Blur()
	m.transcript.Blur()
	m.notes.Blur()
	switch p {
	case paneProviders:
		m.providers.Focus()
	case paneTranscript:
		m.transcript.Focus()
	case paneNotes:
		m.notes.Focus()
	}
}
