// Package app is the Bubble Tea root model. It owns focus, layout, message
// routing and the request lifecycle; it draws nothing itself.
package app

import (
	"context"

	"github.com/Dolmaa24/llmctl/internal/config"
	"github.com/Dolmaa24/llmctl/internal/provider"
	"github.com/Dolmaa24/llmctl/internal/session"
	"github.com/Dolmaa24/llmctl/internal/ui/composer"
	"github.com/Dolmaa24/llmctl/internal/ui/confirm"
	"github.com/Dolmaa24/llmctl/internal/ui/notespane"
	"github.com/Dolmaa24/llmctl/internal/ui/providerform"
	"github.com/Dolmaa24/llmctl/internal/ui/providerpane"
	"github.com/Dolmaa24/llmctl/internal/ui/statusbar"
	"github.com/Dolmaa24/llmctl/internal/ui/switchconfirm"
	"github.com/Dolmaa24/llmctl/internal/ui/transcript"
	tea "github.com/charmbracelet/bubbletea"
)

// Deps are the services the app talks to. Each is an interface from
// llmctl_API_INTERFACE_CONTRACT.md, so a real implementation replaces the demo
// one with no change inside this package.
type Deps struct {
	Registry *provider.Registry
	Configs  config.Store
	Secrets  config.SecretStore

	// Validate checks a provider configuration before it is saved. Nil means
	// no validation is available and configurations are saved unchecked.
	Validate ProviderValidator

	// Kinds are the provider types the add form offers.
	Kinds []providerform.Kind
}

// State is what the panes show at startup.
type State struct {
	Providers []providerpane.Item
	Messages  []session.Message
	Notes     []session.Note
	Checks    []statusbar.Check
}

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
	form       providerform.Model
	remove     confirm.Model

	deps Deps

	sessionID string
	messages  []session.Message

	// Request lifecycle. At most one request is in flight. reqSeq numbers each
	// request so a reply that arrives after the user cancelled — from an
	// adapter that ignored the cancellation — is recognised as stale and
	// dropped rather than appended to the conversation.
	inFlight bool
	reqSeq   int
	cancel   context.CancelFunc

	// Provider form lifecycle. pending holds what the user asked to save while
	// it is validated; it carries the plaintext key, so it is cleared as soon
	// as the save completes or the form closes.
	pending        *pendingSave
	validateSeq    int
	validateCancel context.CancelFunc

	// The provider the open switch modal would switch to.
	switchTarget string

	// The provider the open removal confirmation would remove.
	removeTarget string

	focus  pane
	width  int
	height int

	// Set by layout(), read by View(), so the two cannot disagree about which
	// panes exist at a given width.
	showProviders bool
	showNotes     bool

	quitting bool
}

func New(d Deps, s State) Model {
	m := Model{
		providers:  providerpane.New(s.Providers),
		transcript: transcript.New(s.Messages),
		notes:      notespane.New(s.Notes),
		composer:   composer.New(),
		status:     statusbar.New(s.Checks),
		confirm:    switchconfirm.New(),
		form:       providerform.New(),
		remove:     confirm.New(),
		deps:       d,
		messages:   s.Messages,
		sessionID:  "local",
	}
	// Until storage creates sessions, adopt the id of the history we were given.
	if len(s.Messages) > 0 && s.Messages[0].SessionID != "" {
		m.sessionID = s.Messages[0].SessionID
	}
	if a, ok := m.activeProvider(); ok {
		m.composer.SetTarget(a.ID, a.Model)
	}
	m.setFocus(paneComposer)
	return m
}

func (m Model) Init() tea.Cmd { return nil }
