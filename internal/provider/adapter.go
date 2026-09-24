// Package provider defines the single interface every LLM provider implements.
// No module outside this package may call a provider-specific function or
// branch on provider identity: new providers are added by implementing Adapter
// and registering it, with no other file modified.
package provider

import (
	"context"
	"errors"

	"github.com/Dolmaa24/llmctl/internal/session"
)

// ErrContextTooLarge is returned by SendMessage when the assembled history does
// not fit the target model's context window. Adapters must return this rather
// than silently truncating history, so session/handoff.go can react to the
// problem instead of swallowing it.
var ErrContextTooLarge = errors.New("provider: conversation exceeds model context window")

// Adapter is the contract every provider satisfies identically.
//
// Every method returns an error and must never panic — the diagnostics panel
// depends on catching and displaying provider failures gracefully.
type Adapter interface {
	// Name returns the provider identifier, e.g. "anthropic".
	Name() string

	// Validate checks that stored credentials and connectivity actually work,
	// returning a descriptive error the doctor panel can display.
	Validate(ctx context.Context) error

	// ListModels returns the model identifiers this provider currently exposes,
	// used to validate a model ID before a switch completes.
	ListModels(ctx context.Context) ([]string, error)

	// GetEnvVars returns the environment variables this provider needs
	// exported, decrypted and ready. It does not decide shell syntax — that is
	// the shell package's job.
	GetEnvVars(ctx context.Context) (map[string]string, error)

	// SendMessage sends an already-assembled conversation to this
	// provider/model and returns the assistant's reply.
	SendMessage(ctx context.Context, model string, history []session.Message) (session.Message, error)

	// EstimateTokens approximates the token count for text.
	//
	// Note: per-provider tokenizers differ, so a count from one adapter is not
	// comparable with a count from another. Any measurement that compares
	// strategies across providers must use a single consistent counter rather
	// than this method.
	EstimateTokens(text string) int
}
