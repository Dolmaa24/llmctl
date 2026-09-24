// Package app is the Bubble Tea root model. It owns focus, layout, message
// routing and the request lifecycle; it draws nothing itself.
package app

import (
	"context"

	"github.com/Dolmaa24/llmctl/internal/provider"
	"github.com/Dolmaa24/llmctl/internal/session"
	"github.com/Dolmaa24/llmctl/internal/ui/composer"
	"github.com/Dolmaa24/llmctl/internal/ui/notespane"
	"github.com/Dolmaa24/llmctl/internal/ui/providerpane"
	"github.com/Dolmaa24/llmctl/internal/ui/statusbar"
	"github.com/Dolmaa24/llmctl/internal/ui/switchconfirm"
	"github.com/Dolmaa24/llmctl/internal/ui/transcript"
	tea "github.com/charmbracelet/bubbletea"
)

// pane identifies which component holds focus. The composer comes first
// because it is where the user spends most of their time, and one Tab from it
// reaches the provider list, where switching happens.
type pane int

const (
	paneComposer pane = iota
	paneProviders
	paneTranscript
	paneNotes
	paneCount
)

type Model struct {
	providers  providerpane.Model
	transcript transcript.Model
	notes      notespane.Model
	composer   composer.Model
	status     statusbar.Model
	confirm    switchconfirm.Model

	// registry resolves the active provider to the adapter that talks to it.
	// The app never calls a provider-specific function; it only ever holds a
	// provider.Adapter, which is what lets a real adapter replace the demo one
	// without changing this package.
	registry *provider.Registry

	sessionID string
	messages  []session.Message

	// Request lifecycle. At most one request is in flight. reqSeq numbers each
	// request so a reply that arrives after the user cancelled — from an
	// adapter that ignored the cancellation — is recognised as stale and
	// dropped rather than appended to the conversation.
	inFlight bool
	reqSeq   int
	cancel   context.CancelFunc

	// The provider the open switch modal would switch to.
	switchTarget string

	focus  pane
	width  int
	height int

	// Set by layout(), read by View(), so the two cannot disagree about which
	// panes exist at a given width.
	showProviders bool
	showNotes     bool

	quitting bool
}

func New(
	registry *provider.Registry,
	providers []providerpane.Item,
	messages []session.Message,
	notes []session.Note,
	checks []statusbar.Check,
) Model {
	m := Model{
		providers:  providerpane.New(providers),
		transcript: transcript.New(messages),
		notes:      notespane.New(notes),
		composer:   composer.New(),
		status:     statusbar.New(checks),
		confirm:    switchconfirm.New(),
		registry:   registry,
		messages:   messages,
		sessionID:  "local",
	}
	// Until storage creates sessions, adopt the id of the history we were given.
	if len(messages) > 0 && messages[0].SessionID != "" {
		m.sessionID = messages[0].SessionID
	}
	if a, ok := m.activeProvider(); ok {
		m.composer.SetTarget(a.ID, a.Model)
	}
	m.setFocus(paneComposer)
	return m
}

func (m Model) Init() tea.Cmd { return nil }
