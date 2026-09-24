package app

import (
	"github.com/Dolmaa24/llmctl/internal/session"
	"github.com/Dolmaa24/llmctl/internal/ui/providerpane"
	"github.com/Dolmaa24/llmctl/internal/ui/statusbar"
)

// The setters below are how storage integration feeds real data in: the root
// model takes new state and hands it to the panes, which re-render. No pane
// reads from storage itself.

func (m *Model) SetMessages(msgs []session.Message) {
	m.messages = msgs
	m.transcript.SetMessages(msgs)
}

func (m *Model) SetNotes(notes []session.Note) { m.notes.SetNotes(notes) }

func (m *Model) SetProviders(items []providerpane.Item) { m.providers.SetItems(items) }

func (m *Model) SetChecks(checks []statusbar.Check) { m.status.SetChecks(checks) }

// openSwitch shows the cost comparison for switching to targetID.
func (m *Model) openSwitch(targetID string) {
	// Switching mid-request would leave the reply attributed to a provider
	// the user has already left.
	if m.inFlight {
		m.transcript.SetNotice("wait for the reply, or press esc to cancel it, before switching")
		return
	}
	if active, ok := m.activeProvider(); ok && active.ID == targetID {
		return
	}
	m.switchTarget = targetID
	m.confirm.Show(m.planFor(targetID))
}

// setActive makes id the provider that subsequent messages go to.
func (m *Model) setActive(id string) {
	if id == "" {
		return
	}
	// Copy rather than edit in place: Items returns the pane's own slice, and
	// changing it behind the pane's back would bypass SetItems.
	items := m.providers.Items()
	updated := make([]providerpane.Item, len(items))
	for i, it := range items {
		it.Active = it.ID == id
		updated[i] = it
	}
	m.providers.SetItems(updated)
	if a, ok := m.activeProvider(); ok {
		m.composer.SetTarget(a.ID, a.Model)
	}
}

// activeProvider returns the provider this session is currently using.
func (m Model) activeProvider() (providerpane.Item, bool) {
	for _, it := range m.providers.Items() {
		if it.Active {
			return it, true
		}
	}
	return providerpane.Item{}, false
}

func (m Model) modelFor(id string) string {
	for _, it := range m.providers.Items() {
		if it.ID == id {
			return it.Model
		}
	}
	return ""
}
