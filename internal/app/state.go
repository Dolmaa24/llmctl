package app

import (
	"github.com/Dolmaa24/llmctl/internal/session"
	"github.com/Dolmaa24/llmctl/internal/ui/providerpane"
	"github.com/Dolmaa24/llmctl/internal/ui/statusbar"
)

// The setters below are how Phase 2 feeds real data in: the root model takes
// new state and hands it to the panes, which re-render. No pane reads from
// storage itself.

func (m *Model) SetMessages(msgs []session.Message) {
	m.messages = msgs
	m.transcript.SetMessages(msgs)
}

func (m *Model) SetNotes(notes []session.Note) { m.notes.SetNotes(notes) }

func (m *Model) SetProviders(items []providerpane.Item) { m.providers.SetItems(items) }

func (m *Model) SetChecks(checks []statusbar.Check) { m.status.SetChecks(checks) }

// activeProvider returns the provider this session is currently using.
func (m Model) activeProvider() (providerpane.Item, bool) {
	// SetItems keeps the slice the pane renders, so ask it rather than holding
	// a second copy here that could drift out of step.
	for _, it := range m.providerItems() {
		if it.Active {
			return it, true
		}
	}
	return providerpane.Item{}, false
}

func (m Model) modelFor(id string) string {
	for _, it := range m.providerItems() {
		if it.ID == id {
			return it.Model
		}
	}
	return ""
}

func (m Model) providerItems() []providerpane.Item { return m.providers.Items() }
