package app

import (
	"context"
	"strings"
	"testing"

	"github.com/Dolmaa24/llmctl/internal/config"
	"github.com/Dolmaa24/llmctl/internal/ui/providerpane"
)

func providerItem(h *harness, id string) (providerpane.Item, bool) {
	for _, it := range h.model().providers.Items() {
		if it.ID == id {
			return it, true
		}
	}
	return providerpane.Item{}, false
}

// A new user adds their first provider and can send a message immediately.
func TestFirstRunAddsAProviderAndCanSend(t *testing.T) {
	h := newHarness(t, 120, 32, firstRun())
	h.press("tab", "a")
	out := h.view()
	if !strings.Contains(out, "Add provider") || !strings.Contains(out, "● Anthropic") {
		t.Fatalf("form did not open on Anthropic:\n%s", out)
	}

	h.press("tab") // provider choice -> key
	h.typeText("sk-ant-new-key")
	// Every letter must have reached the key field. The key contains s, a and
	// e, which are shortcuts in the provider list behind the form.
	if got := h.model().form.KeyValue(); got != "sk-ant-new-key" {
		t.Fatalf("key field holds %q: a letter was taken as a shortcut", got)
	}
	h.press("enter")
	if !strings.Contains(h.view(), "connected to Anthropic and saved") {
		t.Fatalf("no success shown:\n%s", h.view())
	}
	h.press("enter") // done

	if h.model().form.Visible() {
		t.Fatal("form still open after done")
	}
	it, ok := providerItem(h, "anthropic")
	if !ok || !it.Active || it.Status != "ok" {
		t.Fatalf("provider row = %+v, want active and ok", it)
	}
	if k, _ := h.deps.Secrets.GetAPIKey("anthropic"); k != "sk-ant-new-key" {
		t.Errorf("saved key = %q", k)
	}
	if c, _ := h.deps.Configs.GetProviderConfig("anthropic"); c.DefaultModel != "claude-opus-5" {
		t.Errorf("saved config = %+v", c)
	}

	h.press("shift+tab") // back to the composer
	h.typeText("hello")
	h.press("enter")
	if got := lastMessage(h); got.Provider != "anthropic" {
		t.Errorf("first message went to %q", got.Provider)
	}
}

// The key must never appear on screen, in any state. The demo recording is
// exactly where a leaked key would end up.
func TestAPIKeyIsNeverRendered(t *testing.T) {
	const secret = "TOPSECRET4f9a"

	h := newHarness(t, 120, 32, firstRun())
	hidden := func(stage string) {
		t.Helper()
		if strings.Contains(h.view(), secret) {
			t.Errorf("key visible while %s", stage)
		}
	}

	h.press("tab", "a", "tab")
	h.typeText("sk-or-" + secret) // wrong provider's prefix: will fail
	hidden("typing")
	if !strings.Contains(h.view(), "•••") {
		t.Error("key field shows no mask while typing")
	}
	h.hold("enter")
	hidden("validating")
	h.release()
	hidden("showing a failure")

	h.press("ctrl+u")
	h.typeText("sk-ant-" + secret)
	h.press("enter")
	hidden("showing success")
	// Checked here, before the user dismisses the success screen: the key is
	// no longer needed once it is saved (SRS NFR-4), and closing the form
	// would clear it anyway, which would hide a save that forgot to.
	if h.model().pending != nil {
		t.Error("the pending save still holds the key after saving")
	}
	h.press("enter")
	hidden("closed")

	if h.model().form.KeyValue() != "" {
		t.Error("the form still holds the key after closing")
	}
}

// Keys are nearly always pasted, and a paste often brings trailing
// whitespace that would make a valid key fail to authenticate.
func TestPastedKeyIsTrimmed(t *testing.T) {
	h := newHarness(t, 120, 32, firstRun())
	h.press("tab", "a", "tab")
	h.typeText("  sk-ant-pasted  ")
	h.press("enter")
	if k, _ := h.deps.Secrets.GetAPIKey("anthropic"); k != "sk-ant-pasted" {
		t.Errorf("saved key = %q, want it trimmed", k)
	}
}

func TestMissingKeyIsCaughtBeforeValidating(t *testing.T) {
	h := newHarness(t, 120, 32, firstRun())
	h.press("tab", "a", "enter")
	if !strings.Contains(h.view(), "an API key is required") {
		t.Errorf("missing key not reported:\n%s", h.view())
	}
	if h.model().pending != nil {
		t.Error("validation started without a key")
	}
}

func TestBadBaseURLIsCaughtBeforeValidating(t *testing.T) {
	h := newHarness(t, 120, 32, firstRun())
	h.press("tab", "a", "tab")
	h.typeText("sk-ant-ok")
	h.press("tab", "ctrl+u") // base URL, cleared
	h.typeText("api.anthropic.com")
	h.press("enter")
	if !strings.Contains(h.view(), "must start with http:// or https://") {
		t.Errorf("bad URL not reported:\n%s", h.view())
	}
}

// A failed validation gives the reason, and the user may save anyway: Ollama,
// for one, is often configured before it is started.
func TestFailureShowsReasonAndCanSaveAnyway(t *testing.T) {
	h := newHarness(t, 120, 32, firstRun())
	h.press("tab", "a", "tab")
	h.typeText("sk-or-wrong-provider")
	h.press("enter")
	if !strings.Contains(h.view(), "this key is not for Anthropic") {
		t.Fatalf("no specific reason shown:\n%s", h.view())
	}
	if _, ok := providerItem(h, "anthropic"); ok {
		t.Fatal("provider saved before the user chose to")
	}

	h.press("enter") // save anyway
	if !strings.Contains(h.view(), "not connecting yet") {
		t.Errorf("save-anyway result not shown:\n%s", h.view())
	}
	if it, ok := providerItem(h, "anthropic"); !ok || it.Status != "fail" {
		t.Errorf("provider row = %+v, want saved with status fail", it)
	}
}

// After a change, Enter must check the new values, not save the old ones.
func TestEditingAfterFailureValidatesAgain(t *testing.T) {
	h := newHarness(t, 120, 32, firstRun())
	h.press("tab", "a", "tab")
	h.typeText("sk-or-wrong-provider")
	h.press("enter") // fails
	h.press("ctrl+u")
	h.typeText("sk-ant-right")
	h.press("enter")
	if !strings.Contains(h.view(), "connected to Anthropic and saved") {
		t.Fatalf("new values were not validated:\n%s", h.view())
	}
	if it, _ := providerItem(h, "anthropic"); it.Status != "ok" {
		t.Errorf("status = %q, want ok", it.Status)
	}
}

// The demo's story end to end: OpenRouter's key has expired, the user saves a
// new one, and messages to OpenRouter start working.
func TestReKeyingTheExpiredProviderFixesIt(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press("tab", "down", "e") // OpenRouter, focus on its key
	out := h.view()
	if !strings.Contains(out, "Update provider") || !strings.Contains(out, "● OpenRouter") {
		t.Fatalf("form did not open on OpenRouter:\n%s", out)
	}
	h.typeText("sk-or-v1-fresh")
	h.press("enter", "enter")

	if it, _ := providerItem(h, "openrouter"); it.Status != "ok" {
		t.Errorf("OpenRouter status = %q, want ok", it.Status)
	}
	if strings.Contains(h.view(), "key expired") {
		t.Error("status bar still reports the expired key")
	}

	h.press("s", "enter", "shift+tab") // switch to OpenRouter
	h.typeText("does it work now?")
	h.press("enter")
	if got := lastMessage(h); got.Provider != "openrouter" || got.Role != "assistant" {
		t.Errorf("message after re-keying: %+v", got)
	}
}

// A blank key means "keep the one saved", both for validating and saving.
func TestBlankKeyKeepsTheSavedOne(t *testing.T) {
	var checkedWith string
	spy := func(_ context.Context, _ config.ProviderConfig, key string) error {
		checkedWith = key
		return nil
	}
	h := newHarness(t, 120, 32, withValidator(spy))
	h.press("tab", "e")             // Anthropic
	h.press("tab", "tab", "ctrl+u") // model field, cleared
	h.typeText("claude-sonnet-5")
	h.press("enter")

	if checkedWith != "sk-ant-demo-0000" {
		t.Errorf("validated with %q, want the saved key", checkedWith)
	}
	if k, _ := h.deps.Secrets.GetAPIKey("anthropic"); k != "sk-ant-demo-0000" {
		t.Errorf("saved key changed to %q", k)
	}
	if c, _ := h.deps.Configs.GetProviderConfig("anthropic"); c.DefaultModel != "claude-sonnet-5" {
		t.Errorf("model not updated: %+v", c)
	}
}

// Changing the active provider's model changes where the next message goes,
// and the transcript marks the change like any other switch.
func TestChangingTheActiveModelMovesTheNextMessage(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press("tab", "down", "down", "e") // Ollama: no key field, focus on URL
	h.press("tab", "ctrl+u")            // model field, cleared
	h.typeText("llama3.1:70b")
	h.press("enter", "enter", "shift+tab")

	h.typeText("same question, bigger model")
	h.press("enter")
	if got := lastMessage(h); got.Model != "llama3.1:70b" {
		t.Errorf("reply from %q, want llama3.1:70b", got.Model)
	}
	if !strings.Contains(h.view(), "switched to ollama: llama3.1:70b") {
		t.Error("no divider marking the model change")
	}
}

// Esc stops a validation, and a result that arrives afterwards is dropped,
// even a successful one: the user walked away from it.
func TestStoppedValidationCannotSaveLater(t *testing.T) {
	approveEverything := func(context.Context, config.ProviderConfig, string) error { return nil }
	h := newHarness(t, 120, 32, firstRun(), withValidator(approveEverything))
	h.press("tab", "a", "tab")
	h.typeText("sk-ant-x")
	h.hold("enter")
	h.press("esc")
	h.release() // the validator now reports success

	if _, ok := providerItem(h, "anthropic"); ok {
		t.Fatal("a stopped validation saved the provider")
	}
	if _, err := h.deps.Secrets.GetAPIKey("anthropic"); err == nil {
		t.Fatal("a stopped validation saved the key")
	}
	if !h.model().form.Visible() || !strings.Contains(h.view(), "validation stopped") {
		t.Errorf("form should be back to editing:\n%s", h.view())
	}
}

func TestClosingTheFormForgetsTheKey(t *testing.T) {
	h := newHarness(t, 120, 32, firstRun())
	h.press("tab", "a", "tab")
	h.typeText("sk-ant-secret")
	h.press("esc")
	if h.model().form.Visible() {
		t.Fatal("esc did not close the form")
	}
	if h.model().form.KeyValue() != "" || h.model().pending != nil {
		t.Error("the key survived closing the form")
	}
	h.press("a")
	if h.model().form.KeyValue() != "" {
		t.Error("reopening the form brought the key back")
	}
}

// A key typed for one provider must never be submitted as another's.
func TestChangingProviderClearsTheKey(t *testing.T) {
	h := newHarness(t, 120, 32, firstRun())
	h.press("tab", "a", "tab")
	h.typeText("sk-ant-secret")
	h.press("shift+tab", "right") // provider choice -> OpenRouter
	if got := h.model().form.KeyValue(); got != "" {
		t.Errorf("key carried across providers: %q", got)
	}
}

func TestOllamaHasNoKeyField(t *testing.T) {
	h := newHarness(t, 120, 32, firstRun())
	h.press("tab", "a", "right", "right")
	out := h.view()
	if !strings.Contains(out, "● Ollama") {
		t.Fatalf("Ollama not selected:\n%s", out)
	}
	if strings.Contains(out, "API key") {
		t.Error("Ollama shows an API key field")
	}
}

func TestOllamaHostIsRespected(t *testing.T) {
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "OLLAMA_HOST" {
				return v
			}
			return ""
		}
	}
	for in, want := range map[string]string{
		"":                 "http://localhost:11434",
		"gpu-box:11434":    "http://gpu-box:11434",
		"https://gpu:8443": "https://gpu:8443",
	} {
		for _, k := range ProviderKinds(env(in)) {
			if k.ID == "ollama" && k.BaseURL != want {
				t.Errorf("OLLAMA_HOST=%q gave %q, want %q", in, k.BaseURL, want)
			}
		}
	}
}

func TestCtrlCQuitsFromTheProviderForm(t *testing.T) {
	h := newHarness(t, 120, 32)
	h.press("tab", "a", "ctrl+c")
	if !h.quitRequested() {
		t.Error("ctrl+c did not quit while the provider form was open")
	}
}

func TestFormFitsTheTerminal(t *testing.T) {
	for _, size := range sizes {
		w, h := size[0], size[1]
		states := map[string]func(*harness){
			"open":   func(x *harness) { x.press("tab", "a") },
			"failed": func(x *harness) { x.press("tab", "e"); x.typeText("sk-or-wrong"); x.press("enter") },
			"validating": func(x *harness) {
				x.press("tab", "e")
				x.typeText("sk-ant-ok")
				x.hold("enter")
			},
		}
		for name, drive := range states {
			x := newHarness(t, w, h)
			drive(x)
			lines := strings.Split(x.view(), "\n")
			if len(lines) != h {
				t.Errorf("%dx%d %s: %d rows, want %d", w, h, name, len(lines), h)
			}
			for i, l := range lines {
				if got := len([]rune(ansiSGR.ReplaceAllString(l, ""))); got > w {
					t.Errorf("%dx%d %s: row %d is %d wide", w, h, name, i, got)
				}
			}
		}
	}
}

// spySecrets counts decryptions and offers HasAPIKey, as the real store
// does once P5's interface change lands.
type spySecrets struct {
	keys map[string]string
	gets int
}

func (s *spySecrets) GetAPIKey(id string) (string, error) {
	s.gets++
	return s.keys[id], nil
}

func (s *spySecrets) SetAPIKey(id, key string) error { s.keys[id] = key; return nil }

func (s *spySecrets) HasAPIKey(id string) (bool, error) { return s.keys[id] != "", nil }

var (
	_ config.SecretStore = (*memSecrets)(nil)
	_ keyChecker         = (*memSecrets)(nil)
)

// Learning that a key exists must not decrypt it when the store can say so
// itself (SRS NFR-4).
func TestHasKeyDoesNotDecryptWhenTheStoreCanCheck(t *testing.T) {
	spy := &spySecrets{keys: map[string]string{"anthropic": "sk-ant-x"}}
	m := Model{deps: Deps{Secrets: spy}}

	if !m.hasKey("anthropic") || m.hasKey("openrouter") {
		t.Error("presence reported wrongly")
	}
	if spy.gets != 0 {
		t.Errorf("decrypted %d keys to check presence", spy.gets)
	}
}

func TestDemoSecretsReportPresence(t *testing.T) {
	s := &memSecrets{keys: map[string]string{"anthropic": "sk-ant-x", "blank": ""}}
	for id, want := range map[string]bool{"anthropic": true, "blank": false, "ollama": false} {
		if got, err := s.HasAPIKey(id); err != nil || got != want {
			t.Errorf("HasAPIKey(%q) = %v, %v; want %v", id, got, err, want)
		}
	}
}
