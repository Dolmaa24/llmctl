// Package storage persists sessions, messages, notes and switch events to a
// local SQLite database. No repository method may call os.Exit or panic on a
// database error — the caller decides how to surface it.
package storage

import (
	"context"

	"github.com/Dolmaa24/llmctl/internal/session"
)

type SessionsRepo interface {
	Create(ctx context.Context, s *session.Session) error
	Get(ctx context.Context, id string) (*session.Session, error)
	UpdateActiveProvider(ctx context.Context, id, provider, model string) error
	List(ctx context.Context) ([]session.Session, error)
}

type MessagesRepo interface {
	Append(ctx context.Context, m *session.Message) error
	ListBySession(ctx context.Context, sessionID string) ([]session.Message, error)
}

type NotesRepo interface {
	Create(ctx context.Context, n *session.Note) error
	ListBySession(ctx context.Context, sessionID string, includeSuperseded bool) ([]session.Note, error)
	MarkSuperseded(ctx context.Context, noteID, supersededByID string) error
}

type SwitchEventsRepo interface {
	Create(ctx context.Context, e *session.SwitchEvent) error
	ListBySession(ctx context.Context, sessionID string) ([]session.SwitchEvent, error)
}

type ExtractionRunsRepo interface {
	Create(ctx context.Context, r *session.ExtractionRun) error
	ListBySession(ctx context.Context, sessionID string) ([]session.ExtractionRun, error)
}
