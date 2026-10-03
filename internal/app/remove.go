package app

import (
	"fmt"

	"github.com/Dolmaa24/llmctl/internal/ui/providerpane"
)

// Removing a provider (SRS FR-1.4) deletes its configuration and its saved
// key, once the user has confirmed (SRS NFR-8).
//
// CONTRACT GAP: config.Store and config.SecretStore declare no delete methods
// in llmctl_API_INTERFACE_CONTRACT.md. The stores on feature/sanket-storage
// have both, as TOMLStore.DeleteProvider and AgeSecretStore.DeleteAPIKey, so
// they are reached through these interfaces, as HasAPIKey is. Once the
// contract declares them, call them directly.
type (
	providerDeleter interface {
		DeleteProvider(providerID string) error
	}
	keyDeleter interface {
		DeleteAPIKey(providerID string) error
	}
)

// askRemove opens the confirmation for removing item, or says why it cannot
// be removed. Nothing is asked that could not then be done.
func (m *Model) askRemove(item providerpane.Item) {
	// Removing the provider the conversation is using would leave the next
	// message with nowhere to go, and switching away from it silently would
	// skip the cost comparison every switch must show (SRS FR-3.5).
	if item.Active {
		m.transcript.SetNotice(fmt.Sprintf("%s is in use: switch to another provider before removing it", item.Name))
		return
	}
	_, canDeleteConfig := m.deps.Configs.(providerDeleter)
	_, canDeleteKey := m.deps.Secrets.(keyDeleter)
	if !canDeleteConfig || !canDeleteKey {
		m.transcript.SetNotice("providers cannot be removed yet: the config stores in use have no way to delete one")
		return
	}

	body := fmt.Sprintf("Its settings will be deleted. You can add %s again later.", item.Name)
	if m.hasKey(item.ID) {
		body = fmt.Sprintf("Its settings and its saved API key will be deleted. You can add %s again later.", item.Name)
	}
	m.removeTarget = item.ID
	m.remove.Show("Remove "+item.Name+"?", body, "remove")
}

// removeProvider deletes the provider the user confirmed.
//
// The key goes first. If deleting it fails, nothing has changed. If deleting
// the configuration then fails, the provider is still listed, without a key,
// where the user can see it and try again. The other order could fail with
// the configuration gone and the key left behind: a secret the user believes
// was deleted, attached to nothing they can see.
func (m *Model) removeProvider() {
	id := m.removeTarget
	m.removeTarget = ""
	item, ok := m.providerItem(id)
	if !ok {
		return
	}
	configs, okConfigs := m.deps.Configs.(providerDeleter)
	secrets, okSecrets := m.deps.Secrets.(keyDeleter)
	if !okConfigs || !okSecrets {
		return // askRemove checked this before asking
	}

	if err := secrets.DeleteAPIKey(id); err != nil {
		m.transcript.SetNotice(fmt.Sprintf("could not remove %s: %v", item.Name, err))
		return
	}
	if err := configs.DeleteProvider(id); err != nil {
		m.transcript.SetNotice(fmt.Sprintf("deleted %s's saved key, but could not remove its settings: %v", item.Name, err))
		return
	}

	var kept []providerpane.Item
	for _, it := range m.providers.Items() {
		if it.ID != id {
			kept = append(kept, it)
		}
	}
	m.providers.SetItems(kept)
	// INTEGRATION POINT (P1): the doctor should stop running this provider's
	// auth check too, or its next results put it back on the bar.
	m.status.RemoveCheck("auth:" + id)
	m.transcript.SetSuccess("removed " + item.Name)
}

// providerItem returns the provider pane's row for id.
func (m Model) providerItem(id string) (providerpane.Item, bool) {
	for _, it := range m.providers.Items() {
		if it.ID == id {
			return it, true
		}
	}
	return providerpane.Item{}, false
}
