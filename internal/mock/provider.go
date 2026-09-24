// Package mock provides in-memory implementations of every interface in
// llmctl_API_INTERFACE_CONTRACT.md, so each module can be built and tested
// against the contract before the real implementation exists.
//
// Every mock is configurable by assigning its function fields; unset fields
// return benign zero values rather than panicking.
package mock

import (
	"context"
	"time"

	"github.com/team260/llmctl/internal/session"
)

// Adapter is a configurable provider.Adapter.
type Adapter struct {
	NameValue     string
	ValidateErr   error
	Models        []string
	EnvVars       map[string]string
	Reply         session.Message
	SendErr       error
	TokensPerChar float64
	SendMessageFn func(ctx context.Context, model string, history []session.Message) (session.Message, error)
}

func (a *Adapter) Name() string { return a.NameValue }

func (a *Adapter) Validate(ctx context.Context) error { return a.ValidateErr }

func (a *Adapter) ListModels(ctx context.Context) ([]string, error) { return a.Models, nil }

func (a *Adapter) GetEnvVars(ctx context.Context) (map[string]string, error) {
	if a.EnvVars == nil {
		return map[string]string{}, nil
	}
	return a.EnvVars, nil
}

func (a *Adapter) SendMessage(ctx context.Context, model string, history []session.Message) (session.Message, error) {
	if a.SendMessageFn != nil {
		return a.SendMessageFn(ctx, model, history)
	}
	if a.SendErr != nil {
		return session.Message{}, a.SendErr
	}
	m := a.Reply
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	if m.Role == "" {
		m.Role = session.RoleAssistant
	}
	m.Provider, m.Model = a.NameValue, model
	return m, nil
}

// EstimateTokens uses a deterministic character ratio so tests are stable.
func (a *Adapter) EstimateTokens(text string) int {
	ratio := a.TokensPerChar
	if ratio == 0 {
		ratio = 0.25
	}
	return int(float64(len(text)) * ratio)
}
