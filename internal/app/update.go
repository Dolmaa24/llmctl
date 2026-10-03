package app

import (
	"github.com/Dolmaa24/llmctl/internal/ui/composer"
	"github.com/Dolmaa24/llmctl/internal/ui/confirm"
	"github.com/Dolmaa24/llmctl/internal/ui/providerform"
	"github.com/Dolmaa24/llmctl/internal/ui/providerpane"
	"github.com/Dolmaa24/llmctl/internal/ui/switchconfirm"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		cmd := m.layout()
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)

	case composer.SubmitMsg:
		return m.send(msg.Text)

	case replyMsg:
		return m.receive(msg)

	case spinner.TickMsg:
		// Both spinners receive every tick; each ignores ticks carrying
		// another spinner's id.
		var a, b tea.Cmd
		m.transcript, a = m.transcript.Update(msg)
		m.form, b = m.form.Update(msg)
		return m, tea.Batch(a, b)

	case providerform.SubmitMsg:
		return m.submitProvider(msg)

	case validatedMsg:
		return m.receiveValidation(msg)

	case providerform.StopMsg:
		m.stopValidation()
		return m, nil

	case providerform.DismissMsg:
		m.closeProviderForm()
		return m, nil

	case providerpane.SelectedMsg:
		m.openSwitch(msg.ID)
		return m, nil

	case switchconfirm.ConfirmedMsg:
		m.setActive(m.switchTarget)
		m.switchTarget = ""
		return m, nil

	case switchconfirm.CancelledMsg:
		m.switchTarget = ""
		return m, nil

	case confirm.ConfirmedMsg:
		m.removeProvider()
		return m, nil

	case confirm.CancelledMsg:
		m.removeTarget = ""
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ctrl+C is checked before any overlay sees the key. An overlay that
	// swallowed it would leave the user no way out but killing the terminal.
	if key.Matches(msg, Keys.ForceQuit) {
		return m.quit()
	}

	// Overlays are exclusive: while one is open it consumes every other key,
	// so a stray Tab cannot move focus behind a form the user is filling in.
	if m.form.Visible() {
		var cmd tea.Cmd
		m.form, cmd = m.form.Update(msg)
		return m, cmd
	}
	if m.confirm.Visible() {
		var cmd tea.Cmd
		m.confirm, cmd = m.confirm.Update(msg)
		return m, cmd
	}
	if m.remove.Visible() {
		var cmd tea.Cmd
		m.remove, cmd = m.remove.Update(msg)
		return m, cmd
	}

	// Bindings that work everywhere, the composer included. None of them is a
	// key the user could mean as text.
	switch {
	case key.Matches(msg, Keys.NextPane):
		cmd := m.cycleFocus(+1)
		return m, cmd
	case key.Matches(msg, Keys.PrevPane):
		cmd := m.cycleFocus(-1)
		return m, cmd
	case key.Matches(msg, Keys.Cancel) && m.inFlight:
		m.cancelRequest()
		return m, nil
	}

	// While the composer has focus, every remaining key is text. This is what
	// stops "q" from quitting halfway through typing "quick question".
	if m.focus == paneComposer {
		var cmd tea.Cmd
		m.composer, cmd = m.composer.Update(msg)
		return m, cmd
	}

	switch {
	case key.Matches(msg, Keys.Quit):
		return m.quit()
	case key.Matches(msg, Keys.Switch) && m.focus == paneProviders:
		if item, ok := m.providers.Selected(); ok {
			m.openSwitch(item.ID)
		}
		return m, nil
	case key.Matches(msg, Keys.Add) && m.focus == paneProviders:
		m.openProviderForm("")
		return m, nil
	case key.Matches(msg, Keys.Edit) && m.focus == paneProviders:
		if item, ok := m.providers.Selected(); ok {
			m.openProviderForm(item.ID)
		}
		return m, nil
	case key.Matches(msg, Keys.Remove) && m.focus == paneProviders:
		if item, ok := m.providers.Selected(); ok {
			m.askRemove(item)
		}
		return m, nil
	}

	var cmd tea.Cmd
	switch m.focus {
	case paneProviders:
		m.providers, cmd = m.providers.Update(msg)
	case paneTranscript:
		m.transcript, cmd = m.transcript.Update(msg)
	case paneNotes:
		m.notes, cmd = m.notes.Update(msg)
	}
	return m, cmd
}

func (m Model) quit() (tea.Model, tea.Cmd) {
	// Cancel in-flight work so its goroutines can unwind, and drop any key the
	// form was holding.
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.closeProviderForm()
	m.quitting = true
	return m, tea.Quit
}

// visible reports whether a pane is currently on screen.
func (m Model) visible(p pane) bool {
	switch p {
	case paneProviders:
		return m.showProviders
	case paneNotes:
		return m.showNotes
	}
	return true
}

// cycleFocus moves focus by step, skipping panes that are hidden on a narrow
// terminal. Focusing a pane the user cannot see would leave them typing into
// nothing.
func (m *Model) cycleFocus(step int) tea.Cmd {
	p := m.focus
	for i := 0; i < int(paneCount); i++ {
		p = pane((int(p) + step + int(paneCount)) % int(paneCount))
		if m.visible(p) {
			return m.setFocus(p)
		}
	}
	return nil
}

func (m *Model) setFocus(p pane) tea.Cmd {
	m.focus = p
	m.providers.Blur()
	m.transcript.Blur()
	m.notes.Blur()
	m.composer.Blur()
	switch p {
	case paneProviders:
		m.providers.Focus()
	case paneTranscript:
		m.transcript.Focus()
	case paneNotes:
		m.notes.Focus()
	case paneComposer:
		return m.composer.Focus()
	}
	return nil
}
