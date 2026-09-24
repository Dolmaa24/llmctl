// Package app is the Bubble Tea root model. It owns focus, layout and message
// routing; it draws nothing itself and holds no business logic.
package app

import (
	"github.com/Dolmaa24/llmctl/internal/session"
	"github.com/Dolmaa24/llmctl/internal/ui/notespane"
	"github.com/Dolmaa24/llmctl/internal/ui/providerpane"
	"github.com/Dolmaa24/llmctl/internal/ui/statusbar"
	"github.com/Dolmaa24/llmctl/internal/ui/switchconfirm"
	"github.com/Dolmaa24/llmctl/internal/ui/transcript"
	tea "github.com/charmbracelet/bubbletea"
)

// pane identifies which component holds focus.
type pane int

const (
	paneProviders pane = iota
	paneTranscript
	paneNotes
	paneCount
)

type Model struct {
	providers  providerpane.Model
	transcript transcript.Model
	notes      notespane.Model
	status     statusbar.Model
	confirm    switchconfirm.Model

	focus  pane
	width  int
	height int

	// Set by layout(), read by View(). Keeping the decision in one place stops
	// the two from disagreeing about which panes exist at a given width.
	showProviders bool
	showNotes     bool

	// Session state the panes render. In Phase 2 this comes from the storage
	// repositories; today it is supplied by the caller.
	messages []session.Message
	quitting bool
}

func New(
	providers []providerpane.Item,
	messages []session.Message,
	notes []session.Note,
	checks []statusbar.Check,
) Model {
	m := Model{
		providers:  providerpane.New(providers),
		transcript: transcript.New(messages),
		notes:      notespane.New(notes),
		status:     statusbar.New(checks),
		confirm:    switchconfirm.New(),
		messages:   messages,
	}
	m.providers.Focus()
	return m
}

func (m Model) Init() tea.Cmd { return nil }
