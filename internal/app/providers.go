package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Dolmaa24/llmctl/internal/config"
	"github.com/Dolmaa24/llmctl/internal/ui/providerform"
	"github.com/Dolmaa24/llmctl/internal/ui/providerpane"
	tea "github.com/charmbracelet/bubbletea"
)

// ProviderValidator checks a provider configuration by connecting with it,
// before the configuration is saved (SRS FR-1.3).
//
// CONTRACT GAP: llmctl_API_INTERFACE_CONTRACT.md has no way to do this.
// Adapter.Validate checks the credentials already stored for a registered
// adapter, and nothing in the contract builds an adapter from an unsaved
// configuration. So a new key could only be checked by saving it first. This
// hook stands in until the provider module (P2) offers the equivalent; its
// signature is what that addition would most naturally look like.
type ProviderValidator func(ctx context.Context, cfg config.ProviderConfig, apiKey string) error

// validateTimeout bounds a validation. It is longer than the 2 s diagnostics
// budget because this one is interactive and may include a TLS handshake; the
// user can stop it sooner with esc.
const validateTimeout = 10 * time.Second

// pendingSave is a configuration awaiting validation. It carries the
// plaintext key, so it lives only until the save completes or the form closes.
type pendingSave struct {
	cfg config.ProviderConfig
	key string // empty: keep the saved key
}

type validatedMsg struct {
	seq int
	err error
}

// ProviderKinds lists the provider types the form offers, with defaults.
// getenv is injected so tests are not affected by the developer's environment.
func ProviderKinds(getenv func(string) string) []providerform.Kind {
	// Respect an existing OLLAMA_HOST, as Ollama's own tools do, so llmctl
	// does not fight a working setup (llmctl_TECHNICAL_ARCHITECTURE.md §6).
	ollama := "http://localhost:11434"
	if h := strings.TrimSpace(getenv("OLLAMA_HOST")); h != "" {
		if !strings.Contains(h, "://") {
			h = "http://" + h
		}
		ollama = h
	}
	return []providerform.Kind{
		{ID: "anthropic", Name: "Anthropic", NeedsKey: true,
			BaseURL: "https://api.anthropic.com", Model: "claude-opus-5"},
		{ID: "openrouter", Name: "OpenRouter", NeedsKey: true,
			BaseURL: "https://openrouter.ai/api/v1", ModelHint: "e.g. meta-llama/llama-3.1-70b-instruct"},
		{ID: "ollama", Name: "Ollama", NeedsKey: false,
			BaseURL: ollama, Model: "llama3.1:8b"},
	}
}

// openProviderForm shows the form, starting on preselect if given.
func (m *Model) openProviderForm(preselect string) {
	saved := map[string]providerform.Saved{}
	for _, k := range m.deps.Kinds {
		cfg, err := m.deps.Configs.GetProviderConfig(k.ID)
		if err != nil {
			continue // not configured
		}
		saved[k.ID] = providerform.Saved{
			BaseURL: cfg.BaseURL,
			Model:   cfg.DefaultModel,
			HasKey:  m.hasKey(k.ID),
		}
	}
	m.form.Open(m.deps.Kinds, saved, preselect)
}

// keyChecker is the presence check P5 adds to config.SecretStore on
// feature/sanket-storage (plan/pr-drafts/PR5-secret-store.md).
type keyChecker interface {
	HasAPIKey(providerID string) (bool, error)
}

// hasKey reports whether a key is saved for id.
//
// It asks the store's HasAPIKey, so no key is decrypted just to learn that it
// exists (SRS NFR-4). Until P5's interface change reaches main,
// config.SecretStore does not declare that method, so it is reached through
// keyChecker, and a store without it falls back to decrypting and discarding
// the key. Once SecretStore declares HasAPIKey, call it directly and delete
// the fallback.
func (m Model) hasKey(id string) bool {
	if kc, ok := m.deps.Secrets.(keyChecker); ok {
		has, err := kc.HasAPIKey(id)
		return err == nil && has
	}
	k, err := m.deps.Secrets.GetAPIKey(id)
	return err == nil && k != ""
}

// submitProvider validates what the form submitted, or saves it at once when
// the user chose to save despite a failed validation.
func (m Model) submitProvider(s providerform.SubmitMsg) (tea.Model, tea.Cmd) {
	r := s.Result
	p := pendingSave{
		cfg: config.ProviderConfig{
			ID:           r.Kind.ID,
			DisplayName:  r.Kind.Name,
			BaseURL:      r.BaseURL,
			DefaultModel: r.Model,
			Enabled:      true,
		},
		key: r.APIKey,
	}

	if s.Force || m.deps.Validate == nil {
		reason, detail := "", fmt.Sprintf("%s saved", p.cfg.DisplayName)
		if s.Force {
			reason = "saved without a working connection"
			detail = fmt.Sprintf("%s saved, but it is not connecting yet", p.cfg.DisplayName)
		}
		if err := m.commitProvider(p, s.Force, reason); err != nil {
			m.form.SetFailed("could not save: " + err.Error())
			return m, nil
		}
		m.form.SetSaved(detail)
		return m, nil
	}

	// A blank key means "keep the saved one", and that is the key to check.
	checkKey := p.key
	if checkKey == "" && r.Kind.NeedsKey {
		k, err := m.deps.Secrets.GetAPIKey(p.cfg.ID)
		if err != nil || k == "" {
			m.form.SetFailed("no saved key to keep; enter one")
			return m, nil
		}
		checkKey = k
	}

	m.pending = &p
	m.validateSeq++
	ctx, cancel := context.WithTimeout(context.Background(), validateTimeout)
	m.validateCancel = cancel

	spin := m.form.SetValidating()
	check := validateCmd(ctx, m.validateSeq, m.deps.Validate, p.cfg, checkKey)
	return m, tea.Batch(spin, check)
}

// validateCmd runs a validation off the update loop. A panic in the validator
// becomes a failure, for the same reason as in request.
func validateCmd(ctx context.Context, seq int, v ProviderValidator, cfg config.ProviderConfig, key string) tea.Cmd {
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = validatedMsg{seq: seq, err: fmt.Errorf("validator panicked: %v", r)}
			}
		}()
		return validatedMsg{seq: seq, err: v(ctx, cfg, key)}
	}
}

// receiveValidation handles a finished validation.
func (m Model) receiveValidation(v validatedMsg) (tea.Model, tea.Cmd) {
	// A result for a validation the user stopped, or for a form they closed,
	// is dropped. Without this, a slow validator could save a provider after
	// the user had walked away from the form.
	if v.seq != m.validateSeq || m.pending == nil {
		return m, nil
	}
	m.stopValidation()

	if v.err != nil {
		reason := v.err.Error()
		if errors.Is(v.err, context.DeadlineExceeded) {
			reason = fmt.Sprintf("no response after %s; check the base URL and your network", validateTimeout)
		}
		// pending is kept, so "save anyway" can still save it.
		m.form.SetFailed(reason)
		return m, nil
	}

	p := *m.pending
	if err := m.commitProvider(p, false, ""); err != nil {
		m.form.SetFailed("could not save: " + err.Error())
		return m, nil
	}
	m.form.SetSaved(fmt.Sprintf("connected to %s and saved", p.cfg.DisplayName))
	return m, nil
}

// commitProvider saves a configuration and updates the panes that show it.
func (m *Model) commitProvider(p pendingSave, failing bool, reason string) error {
	// The key is saved first: if that fails, nothing else has changed. The
	// contract offers no transaction across the two stores, so a failure
	// between them can still leave a key with no configuration; that is for
	// the config module (P5) to make atomic.
	if p.key != "" {
		if err := m.deps.Secrets.SetAPIKey(p.cfg.ID, p.key); err != nil {
			return err
		}
	}
	if err := m.deps.Configs.SetProviderConfig(p.cfg); err != nil {
		return err
	}
	m.pending = nil // drops the plaintext key

	status := "ok"
	if failing {
		status = "fail"
	}
	m.upsertProvider(p.cfg, status)
	m.status.SetCheck("auth:"+p.cfg.ID, status, reason)
	return nil
}

// upsertProvider updates a provider's row, or adds one for a new provider.
func (m *Model) upsertProvider(cfg config.ProviderConfig, status string) {
	items := append([]providerpane.Item(nil), m.providers.Items()...)
	_, hadActive := m.activeProvider()

	found := false
	for i := range items {
		if items[i].ID == cfg.ID {
			items[i].Name, items[i].Model, items[i].Status = cfg.DisplayName, cfg.DefaultModel, status
			found = true
		}
	}
	if !found {
		// The first provider added becomes active, so a new user can send a
		// message straight away. Adding another never switches silently.
		items = append(items, providerpane.Item{
			ID: cfg.ID, Name: cfg.DisplayName, Model: cfg.DefaultModel,
			Status: status, Active: !hadActive,
		})
	}
	m.providers.SetItems(items)

	// Changing the active provider's model changes where the next message goes.
	if a, ok := m.activeProvider(); ok {
		m.composer.SetTarget(a.ID, a.Model)
	}
}

// stopValidation cancels a running validation. Bumping validateSeq makes any
// result still on its way stale.
func (m *Model) stopValidation() {
	if m.validateCancel != nil {
		m.validateCancel()
		m.validateCancel = nil
	}
	m.validateSeq++
}

// closeProviderForm closes the form and forgets everything it held.
func (m *Model) closeProviderForm() {
	m.stopValidation()
	m.pending = nil
	m.form.Close()
}
