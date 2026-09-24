package app

import (
	"time"

	"github.com/Dolmaa24/llmctl/internal/session"
	"github.com/Dolmaa24/llmctl/internal/ui/providerpane"
	"github.com/Dolmaa24/llmctl/internal/ui/statusbar"
)

// DemoState returns hardcoded state for developing the UI before the storage
// repositories and provider adapters exist.
//
// It is intentionally shaped like real data — a mid-session provider switch,
// a superseded note, a failing check — because a UI developed only against
// tidy data breaks the first time it meets the real thing.
func DemoState() ([]providerpane.Item, []session.Message, []session.Note, []statusbar.Check) {
	providers := []providerpane.Item{
		{ID: "anthropic", Name: "Anthropic", Model: "claude-opus-5", Status: "ok", LatencyM: 412, Active: true},
		{ID: "openrouter", Name: "OpenRouter", Model: "meta-llama/llama-3.1-70b", Status: "warn", LatencyM: 1840},
		{ID: "ollama", Name: "Ollama", Model: "llama3.1:8b", Status: "fail"},
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
		{Name: "ollama", Status: "fail", Message: "connection refused on :11434"},
	}

	return providers, messages, notes, checks
}

// DemoPlan supplies the numbers the cost modal shows until costestimate is
// wired in. The extraction figure is present so the modal is developed against
// the honest comparison rather than a handoff-only one.
func demoPlanNumbers() (fullReplay, distilled, extraction, notesCount, rawTurns int) {
	return 14500, 2800, 1100, 4, 3
}
