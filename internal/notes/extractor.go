// Package notes turns recent conversation turns into structured Note records.
// It imports session for the canonical types; session never imports notes.
package notes

import (
	"context"

	"github.com/Dolmaa24/llmctl/internal/session"
)

// Extractor produces new notes from recent conversation.
//
// Implementations may call a provider adapter internally (typically a small,
// cheap model) — that is an implementation detail, not part of the contract.
//
// Extraction must operate on the turns since the last extraction, never on the
// full transcript: re-extracting everything on each pass makes extraction cost
// grow quadratically with session length and silently exceed whatever the
// distilled handoff saves.
type Extractor interface {
	Extract(ctx context.Context, recent []session.Message, existing []session.Note) ([]session.Note, error)
}
