package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/Dolmaa24/llmctl/internal/config"
)

// From the composer, Tab reaches the provider list with Anthropic selected;
// Down selects OpenRouter. Ollama, one further down, is the active provider.
var toOpenRouter = []string{"tab", "down"}

func TestRemovingAProviderAsksFirst(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press(append(toOpenRouter, "r")...)

	out := h.view()
	if !strings.Contains(out, "Remove OpenRouter?") || !strings.Contains(out, "saved API key will be deleted") {
		t.Fatalf("no confirmation naming the provider and its key:\n%s", out)
	}
	if _, err := h.deps.Configs.GetProviderConfig("openrouter"); err != nil {
		t.Error("removed before the user confirmed")
	}
}

// Confirming deletes the settings and the key, and takes the provider off
// the list and its auth check off the status bar. Nothing else changes.
func TestConfirmingRemovesTheProvider(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press(append(toOpenRouter, "r", "y")...)

	if _, ok := providerItem(h, "openrouter"); ok {
		t.Error("OpenRouter is still listed")
	}
	if _, err := h.deps.Configs.GetProviderConfig("openrouter"); err == nil {
		t.Error("OpenRouter's settings were not deleted")
	}
	if has, _ := h.deps.Secrets.(*memSecrets).HasAPIKey("openrouter"); has {
		t.Error("OpenRouter's key was not deleted")
	}
	if strings.Contains(h.view(), "auth:openrouter") {
		t.Error("the status bar still reports on the removed provider")
	}
	if n := h.model().transcript.Notice(); !strings.Contains(n, "removed OpenRouter") {
		t.Errorf("notice = %q, want the removal confirmed", n)
	}

	if _, ok := providerItem(h, "anthropic"); !ok {
		t.Error("Anthropic was removed too")
	}
	if k, _ := h.deps.Secrets.GetAPIKey("anthropic"); k == "" {
		t.Error("Anthropic's key was deleted too")
	}
}

// Only y confirms. Enter is the key people press to get through every
// dialog, so it must not delete anything (SRS NFR-8).
func TestOnlyYConfirmsRemoval(t *testing.T) {
	for _, k := range []string{"enter", "r", "n", "esc", "q"} {
		h := newHarness(t, 120, 32)
		h.press(append(toOpenRouter, "r", k)...)
		if _, ok := providerItem(h, "openrouter"); !ok {
			t.Errorf("%s removed the provider", k)
		}
		if _, err := h.deps.Configs.GetProviderConfig("openrouter"); err != nil {
			t.Errorf("%s deleted the settings", k)
		}
	}
}

func TestCancellingClosesTheRemovalDialog(t *testing.T) {
	for _, k := range []string{"n", "esc", "q"} {
		h := newHarness(t, 120, 32)
		h.press(append(toOpenRouter, "r", k)...)
		if h.model().remove.Visible() {
			t.Errorf("%s left the dialog open", k)
		}
		if h.quitRequested() {
			t.Errorf("%s quit the program instead of closing the dialog", k)
		}
	}
}

func TestCtrlCQuitsFromTheRemovalDialog(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press(append(toOpenRouter, "r", "ctrl+c")...)
	if !h.quitRequested() {
		t.Error("ctrl+c did not quit while the dialog was open")
	}
}

// The provider in use cannot be removed: the next message would have nowhere
// to go, and switching away silently would skip the cost comparison.
func TestTheActiveProviderCannotBeRemoved(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press("tab", "down", "down", "r") // Ollama

	if h.model().remove.Visible() {
		t.Fatal("asked to confirm a removal that must not happen")
	}
	if n := h.model().transcript.Notice(); !strings.Contains(n, "Ollama is in use") {
		t.Errorf("notice = %q, want why it cannot be removed", n)
	}
	if _, ok := providerItem(h, "ollama"); !ok {
		t.Error("the active provider was removed")
	}
}

// After switching away, the provider can be removed. Ollama has no key, so
// the dialog does not mention one.
func TestARemovedKeylessProviderMentionsNoKey(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press("tab", "s", "enter") // switch to Anthropic
	h.press("down", "down", "r") // Ollama

	out := h.view()
	if !strings.Contains(out, "Remove Ollama?") {
		t.Fatalf("no confirmation for Ollama:\n%s", out)
	}
	if strings.Contains(out, "API key") {
		t.Errorf("the dialog mentions a key Ollama does not have:\n%s", out)
	}
	h.press("y")
	if _, ok := providerItem(h, "ollama"); ok {
		t.Error("Ollama is still listed")
	}
}

func TestProviderListOffersRemove(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press("tab")
	if !strings.Contains(h.view(), "r remove") {
		t.Error("the help bar does not offer removal in the provider list")
	}
}

// Stores with only the contract's methods cannot delete, so nothing is asked
// that could not then be carried out.
type (
	contractConfigs struct{ config.Store }
	contractSecrets struct{ config.SecretStore }
)

func TestRemovalNeedsStoresThatCanDelete(t *testing.T) {
	h := newHarness(t, 120, 32, func(d *Deps, _ *State) {
		d.Configs = contractConfigs{d.Configs}
		d.Secrets = contractSecrets{d.Secrets}
	})
	h.press(append(toOpenRouter, "r")...)

	if h.model().remove.Visible() {
		t.Fatal("asked to confirm a removal the stores cannot carry out")
	}
	if n := h.model().transcript.Notice(); !strings.Contains(n, "cannot be removed yet") {
		t.Errorf("notice = %q, want why removal is unavailable", n)
	}
	if _, err := h.deps.Configs.GetProviderConfig("openrouter"); err != nil {
		t.Error("the provider was removed anyway")
	}
}

type (
	keyDeleteFails    struct{ *memSecrets }
	configDeleteFails struct{ *memConfigs }
)

func (keyDeleteFails) DeleteAPIKey(string) error      { return errors.New("vault is locked") }
func (configDeleteFails) DeleteProvider(string) error { return errors.New("disk is full") }

// When the key cannot be deleted, nothing is removed.
func TestKeyDeleteFailureRemovesNothing(t *testing.T) {
	h := newHarness(t, 120, 32, func(d *Deps, _ *State) {
		d.Secrets = keyDeleteFails{d.Secrets.(*memSecrets)}
	})
	h.press(append(toOpenRouter, "r", "y")...)

	if _, err := h.deps.Configs.GetProviderConfig("openrouter"); err != nil {
		t.Error("the settings were deleted although the key could not be")
	}
	if _, ok := providerItem(h, "openrouter"); !ok {
		t.Error("the provider left the list")
	}
	if n := h.model().transcript.Notice(); !strings.Contains(n, "could not remove OpenRouter") || !strings.Contains(n, "vault is locked") {
		t.Errorf("notice = %q, want the failure and its reason", n)
	}
}

// The key is deleted before the settings, so a failure between the two
// leaves the provider listed and fixable, never a key nobody can see.
func TestSettingsDeleteFailureKeepsTheProviderVisible(t *testing.T) {
	h := newHarness(t, 120, 32, func(d *Deps, _ *State) {
		d.Configs = configDeleteFails{d.Configs.(*memConfigs)}
	})
	h.press(append(toOpenRouter, "r", "y")...)

	if has, _ := h.deps.Secrets.(*memSecrets).HasAPIKey("openrouter"); has {
		t.Error("the key survived: removal did not start with it")
	}
	if _, ok := providerItem(h, "openrouter"); !ok {
		t.Error("the provider left the list although its settings remain")
	}
	if n := h.model().transcript.Notice(); !strings.Contains(n, "could not remove its settings") {
		t.Errorf("notice = %q, want the partial failure explained", n)
	}
}
