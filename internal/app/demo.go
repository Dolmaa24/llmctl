package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Dolmaa24/llmctl/internal/config"
	"github.com/Dolmaa24/llmctl/internal/provider"
	"github.com/Dolmaa24/llmctl/internal/session"
	"github.com/Dolmaa24/llmctl/internal/ui/providerpane"
	"github.com/Dolmaa24/llmctl/internal/ui/statusbar"
)

// DemoState returns hardcoded state for developing the UI before the storage
// repositories and provider adapters exist.
//
// It tells one consistent story, because a UI developed against tidy or
// contradictory data breaks the first time it meets the real thing. The
// session began on Anthropic and switched to a local Ollama model, which is
// still active. OpenRouter's key has expired: the diagnostics catch it, the
// provider pane shows it, and a message sent there fails the way a real one
// would.
func DemoState() State {
	providers := []providerpane.Item{
		{ID: "anthropic", Name: "Anthropic", Model: "claude-opus-5", Status: "ok", LatencyM: 412},
		{ID: "openrouter", Name: "OpenRouter", Model: "meta-llama/llama-3.1-70b", Status: "fail"},
		{ID: "ollama", Name: "Ollama", Model: "llama3.1:8b", Status: "ok", LatencyM: 35, Active: true},
	}

	t := time.Now().Add(-30 * time.Minute)
	at := func(i int) time.Time { return t.Add(time.Duration(i) * time.Minute) }

	messages := []session.Message{
		{SessionID: "demo", SequenceNum: 0, Role: session.RoleUser,
			Content: "The refill timer is dropping ticks under load. Where should I look first?", CreatedAt: at(0)},
		{SessionID: "demo", SequenceNum: 1, Role: session.RoleAssistant,
			Provider: "anthropic", Model: "claude-opus-5",
			Content: "Start with how the timer state is mutated. If two goroutines read-modify-write the counter without synchronisation you will lose increments silently under contention.", CreatedAt: at(1)},
		{SessionID: "demo", SequenceNum: 2, Role: session.RoleUser,
			Content: "It uses a mutex around the whole refill block.", CreatedAt: at(2)},
		{SessionID: "demo", SequenceNum: 3, Role: session.RoleAssistant,
			Provider: "anthropic", Model: "claude-opus-5",
			Content: "Then contention is the likely cause rather than a race. An atomic compare-and-swap on the counter removes the lock from the hot path entirely.", CreatedAt: at(3)},
		// The switch: everything below is attributed to a different provider,
		// and the transcript must show a divider here.
		{SessionID: "demo", SequenceNum: 4, Role: session.RoleUser,
			Content: "Switched to a local model to keep iterating cheaply. Can you write the CAS loop?", CreatedAt: at(4)},
		{SessionID: "demo", SequenceNum: 5, Role: session.RoleAssistant,
			Provider: "ollama", Model: "llama3.1:8b",
			Content: "Here is the compare-and-swap refill loop, retrying until the swap succeeds. It keeps the fast path lock-free while preserving the invariant you established earlier.", CreatedAt: at(5)},
	}

	supersededBy := "n-004"
	notes := []session.Note{
		{ID: "n-001", SessionID: "demo", Content: "Refill timer drops ticks under concurrent load",
			Provider: "anthropic", Model: "claude-opus-5", Tags: []string{"problem", "concurrency"}, CreatedAt: at(1)},
		{ID: "n-002", SessionID: "demo", Content: "Timer state is guarded by a mutex around the whole refill block",
			Provider: "anthropic", Model: "claude-opus-5", Tags: []string{"fact"}, CreatedAt: at(2)},
		// Superseded: an early hypothesis that a later turn ruled out. The pane
		// must hide it, exactly as handoff assembly would.
		{ID: "n-003", SessionID: "demo", Content: "Suspected a data race on the counter",
			Provider: "anthropic", Model: "claude-opus-5", Tags: []string{"hypothesis"},
			SupersededBy: &supersededBy, CreatedAt: at(2)},
		{ID: "n-004", SessionID: "demo", Content: "Ruled out: not a race, it is lock contention",
			Provider: "anthropic", Model: "claude-opus-5", Tags: []string{"decision", "concurrency"}, CreatedAt: at(3)},
		{ID: "n-005", SessionID: "demo", Content: "Decided: atomic CAS for the refill timer, no mutex on the hot path",
			Provider: "anthropic", Model: "claude-opus-5", Tags: []string{"decision", "concurrency"}, CreatedAt: at(3)},
	}

	checks := []statusbar.Check{
		{Name: "wsl", Status: "ok"},
		{Name: "network", Status: "ok"},
		{Name: "auth:anthropic", Status: "ok"},
		{Name: "auth:openrouter", Status: "fail", Message: "key expired"},
		{Name: "ollama", Status: "ok"},
	}

	return State{Providers: providers, Messages: messages, Notes: notes, Checks: checks}
}

// DemoDeps returns stand-in services: adapters for the three providers,
// in-memory config and key stores, and a validator. Each satisfies the same
// contract interface as the real one, so replacing them is a change to
// main.go and nothing else.
//
// The adapters read the demo key store rather than failing on a fixed flag,
// so the demo story can be completed: OpenRouter fails while its saved key is
// the expired one, and works once the user saves a new key in the form.
func DemoDeps(delay time.Duration) Deps {
	configs := &memConfigs{rows: map[string]config.ProviderConfig{
		"anthropic":  {ID: "anthropic", DisplayName: "Anthropic", BaseURL: "https://api.anthropic.com", DefaultModel: "claude-opus-5", Enabled: true},
		"openrouter": {ID: "openrouter", DisplayName: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", DefaultModel: "meta-llama/llama-3.1-70b", Enabled: true},
		"ollama":     {ID: "ollama", DisplayName: "Ollama", BaseURL: "http://localhost:11434", DefaultModel: "llama3.1:8b", Enabled: true},
	}}
	secrets := &memSecrets{keys: map[string]string{
		"anthropic":  "sk-ant-demo-0000",
		"openrouter": "sk-or-demo-expired",
	}}

	reg := provider.NewRegistry()
	for _, id := range []string{"anthropic", "openrouter", "ollama"} {
		id := id
		reg.Register(demoAdapter{
			id:    id,
			delay: delay,
			check: func() error { return demoKeyProblem(secrets, id) },
		})
	}

	return Deps{
		Registry: reg,
		Configs:  configs,
		Secrets:  secrets,
		Validate: demoValidator(delay),
		Kinds:    ProviderKinds(func(string) string { return "" }),
	}
}

// demoKeyProblem is what a real provider would say about the saved key.
func demoKeyProblem(secrets config.SecretStore, id string) error {
	if id == "ollama" {
		return nil // no key
	}
	key, err := secrets.GetAPIKey(id)
	if err != nil || key == "" {
		return errors.New("no API key saved")
	}
	if strings.Contains(key, "expired") {
		return errors.New("authentication failed: key expired")
	}
	return nil
}

// demoValidator accepts a key only if it has the right provider's prefix and
// is not the expired one. Nothing leaves the machine: a demo that made real
// network calls would surprise whoever ran it.
func demoValidator(delay time.Duration) ProviderValidator {
	return func(ctx context.Context, cfg config.ProviderConfig, key string) error {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
		prefix := map[string]string{"anthropic": "sk-ant-", "openrouter": "sk-or-"}[cfg.ID]
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			return fmt.Errorf("authentication failed: this key is not for %s (%s keys start with %s)",
				cfg.DisplayName, cfg.DisplayName, prefix)
		}
		if strings.Contains(key, "expired") {
			return errors.New("authentication failed: key expired")
		}
		return nil
	}
}

type demoAdapter struct {
	id    string
	delay time.Duration
	check func() error
}

func (d demoAdapter) Name() string                       { return d.id }
func (d demoAdapter) Validate(ctx context.Context) error { return d.check() }
func (d demoAdapter) EstimateTokens(text string) int     { return len(text) / 4 }

func (d demoAdapter) ListModels(context.Context) ([]string, error) { return nil, nil }

func (d demoAdapter) GetEnvVars(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

// SendMessage waits out the delay, honouring cancellation, then answers with a
// reply that says plainly it is a demo, so it cannot be mistaken for model
// output in a screenshot or a demo recording.
func (d demoAdapter) SendMessage(ctx context.Context, model string, history []session.Message) (session.Message, error) {
	select {
	case <-time.After(d.delay):
	case <-ctx.Done():
		return session.Message{}, ctx.Err()
	}
	if err := d.check(); err != nil {
		return session.Message{}, err
	}

	asked := ""
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == session.RoleUser {
			asked = history[i].Content
			break
		}
	}
	if len(asked) > 80 {
		asked = asked[:79] + "…"
	}

	return session.Message{
		Role:     session.RoleAssistant,
		Provider: d.id,
		Model:    model,
		Content: fmt.Sprintf("Demo reply: no provider is connected yet, so nothing was sent anywhere. "+
			"You wrote %q. This turn carried %d messages of history.", asked, len(history)),
		CreatedAt: time.Now(),
	}, nil
}

// memConfigs is an in-memory config.Store for the demo.
type memConfigs struct {
	mu   sync.Mutex
	rows map[string]config.ProviderConfig
}

func (s *memConfigs) GetProviderConfig(id string) (config.ProviderConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.rows[id]
	if !ok {
		return config.ProviderConfig{}, fmt.Errorf("provider %q is not configured", id)
	}
	return c, nil
}

func (s *memConfigs) SetProviderConfig(c config.ProviderConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[c.ID] = c
	return nil
}

// DeleteProvider removes a provider's configuration, failing for one that is
// not configured, as the storage module's TOMLStore does.
func (s *memConfigs) DeleteProvider(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rows[id]; !ok {
		return fmt.Errorf("provider %q is not configured", id)
	}
	delete(s.rows, id)
	return nil
}

// memSecrets is an in-memory config.SecretStore for the demo. Unlike the real
// store it does not encrypt, which is acceptable only because every key it
// holds is fake and nothing is written to disk.
type memSecrets struct {
	mu   sync.Mutex
	keys map[string]string
}

func (s *memSecrets) GetAPIKey(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.keys[id]
	if !ok {
		return "", fmt.Errorf("no key saved for %q", id)
	}
	return k, nil
}

func (s *memSecrets) SetAPIKey(id, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[id] = key
	return nil
}

// HasAPIKey reports whether a key is saved for id without returning it, as
// the real store does from its index.
func (s *memSecrets) HasAPIKey(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keys[id] != "", nil
}

// DeleteAPIKey removes a provider's key. Removing a key that was never saved
// is not an error, as in the storage module's AgeSecretStore.
func (s *memSecrets) DeleteAPIKey(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.keys, id)
	return nil
}

// demoPlanNumbers supplies the figures the cost modal shows until
// costestimate is wired in. The extraction figure is present so the modal is
// developed against the honest comparison rather than a handoff-only one.
func demoPlanNumbers() (fullReplay, distilled, extraction, notesCount, rawTurns int) {
	return 14500, 2800, 1100, 4, 3
}
