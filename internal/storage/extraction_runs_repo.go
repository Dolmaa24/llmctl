package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Dolmaa24/llmctl/internal/session"
)

var _ ExtractionRunsRepo = (*extractionRunsRepo)(nil)

// extractionRunsRepo persists the record of note-extraction passes, so that
// where the next pass starts, and what extraction has cost so far, both
// survive a restart. It has the shape of notes.Ledger and plugs in directly.
type extractionRunsRepo struct{ db *sql.DB }

// Create stores a run and one link per note it wrote, in one transaction: a
// run is never visible with only some of its notes attached. The notes must
// already exist, because the runner stores a pass's notes before recording
// the run. A failed pass (Err set) is stored like any other; it was paid for.
func (r *extractionRunsRepo) Create(ctx context.Context, run *session.ExtractionRun) error {
	if run == nil {
		return fmt.Errorf("%w: extraction run is nil", ErrInvalid)
	}
	if err := ensureID("extraction run id", &run.ID); err != nil {
		return err
	}
	if err := checkProviderModel("", run.Provider, run.Model); err != nil {
		return err
	}
	if run.Provider == "" || run.Model == "" || run.PromptVersion == "" {
		return fmt.Errorf("%w: an extraction run needs a provider, model and prompt version", ErrInvalid)
	}
	if err := checkName("prompt version", run.PromptVersion, maxNameBytes); err != nil {
		return err
	}
	if err := checkContent("error", run.Err); err != nil {
		return err
	}
	for field, n := range map[string]int{
		"through sequence number": run.ThroughSequenceNum,
		"input tokens":            run.InputTokens,
		"output tokens":           run.OutputTokens,
	} {
		if err := checkCount(field, n); err != nil {
			return err
		}
	}
	for _, noteID := range run.NoteIDs {
		if err := checkID("note id", noteID); err != nil {
			return err
		}
	}
	run.CreatedAt = orNow(run.CreatedAt)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: recording extraction run: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO extraction_runs (id, session_id, through_sequence_num, provider, model,
		   prompt_version, input_tokens, output_tokens, error, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.SessionID, run.ThroughSequenceNum, run.Provider, run.Model,
		run.PromptVersion, run.InputTokens, run.OutputTokens, run.Err, encodeTime(run.CreatedAt))
	if err != nil {
		return fmt.Errorf("storage: recording extraction run in session %s: %w", run.SessionID, err)
	}
	for _, noteID := range run.NoteIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO extraction_run_notes (note_id, run_id) VALUES (?, ?)`, noteID, run.ID); err != nil {
			return fmt.Errorf("storage: linking note %s to extraction run %s: %w", noteID, run.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: recording extraction run: %w", err)
	}
	return nil
}

// ListBySession returns a session's runs oldest first, each with the IDs of
// the notes it wrote in the order they were written.
func (r *extractionRunsRepo) ListBySession(ctx context.Context, sessionID string) ([]session.ExtractionRun, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, session_id, through_sequence_num, provider, model, prompt_version,
		   input_tokens, output_tokens, error, created_at
		 FROM extraction_runs WHERE session_id = ? ORDER BY created_at, rowid`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("storage: listing extraction runs: %w", err)
	}
	var out []session.ExtractionRun
	index := map[string]int{}
	for rows.Next() {
		var run session.ExtractionRun
		var created dbTime
		if err := rows.Scan(&run.ID, &run.SessionID, &run.ThroughSequenceNum, &run.Provider, &run.Model,
			&run.PromptVersion, &run.InputTokens, &run.OutputTokens, &run.Err, &created); err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: listing extraction runs: %w", err)
		}
		run.CreatedAt = created.Time
		index[run.ID] = len(out)
		out = append(out, run)
	}
	// Closed before the next query: the database uses a single connection.
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("storage: listing extraction runs: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: listing extraction runs: %w", err)
	}

	links, err := r.db.QueryContext(ctx,
		`SELECT l.run_id, l.note_id
		 FROM extraction_run_notes l JOIN extraction_runs r ON r.id = l.run_id
		 WHERE r.session_id = ? ORDER BY l.rowid`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("storage: listing extraction run notes: %w", err)
	}
	defer links.Close()
	for links.Next() {
		var runID, noteID string
		if err := links.Scan(&runID, &noteID); err != nil {
			return nil, fmt.Errorf("storage: listing extraction run notes: %w", err)
		}
		if i, ok := index[runID]; ok {
			out[i].NoteIDs = append(out[i].NoteIDs, noteID)
		}
	}
	return out, links.Err()
}
