package mock

import (
	"context"
	"slices"
	"sync"

	"github.com/Dolmaa24/llmctl/internal/notes"
)

// Ledger is an in-memory notes.Ledger. The app can use it too, until storage
// has an extraction_runs table; a restart then forgets how far extraction
// has read.
type Ledger struct {
	mu   sync.Mutex
	rows []notes.Run
}

func NewLedger() *Ledger { return &Ledger{} }

func (l *Ledger) Create(ctx context.Context, r *notes.Run) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	run := *r
	run.NoteIDs = slices.Clone(r.NoteIDs)
	l.rows = append(l.rows, run)
	return nil
}

func (l *Ledger) ListBySession(ctx context.Context, sessionID string) ([]notes.Run, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []notes.Run
	for _, r := range l.rows {
		if r.SessionID == sessionID {
			r.NoteIDs = slices.Clone(r.NoteIDs)
			out = append(out, r)
		}
	}
	return out, nil
}
