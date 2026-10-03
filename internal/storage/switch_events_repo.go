package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Dolmaa24/llmctl/internal/session"
)

var _ SwitchEventsRepo = (*switchEventsRepo)(nil)

type switchEventsRepo struct{ db *sql.DB }

func (r *switchEventsRepo) Create(ctx context.Context, e *session.SwitchEvent) error {
	if e == nil {
		return fmt.Errorf("%w: switch event is nil", ErrInvalid)
	}
	if err := ensureID("switch event id", &e.ID); err != nil {
		return err
	}
	if err := checkProviderModel("from ", e.FromProvider, e.FromModel); err != nil {
		return err
	}
	if err := checkProviderModel("to ", e.ToProvider, e.ToModel); err != nil {
		return err
	}
	if e.ToProvider == "" || e.ToModel == "" {
		return fmt.Errorf("%w: a switch needs a target provider and model", ErrInvalid)
	}
	for field, n := range map[string]int{
		"notes sent":           e.NotesSentCount,
		"raw turns sent":       e.RawTurnsSentCount,
		"full replay estimate": e.EstimatedTokensFullReplay,
		"distilled estimate":   e.EstimatedTokensDistilled,
	} {
		if err := checkCount(field, n); err != nil {
			return err
		}
	}
	e.CreatedAt = orNow(e.CreatedAt)
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO switch_events (id, session_id, from_provider, from_model, to_provider, to_model,
		   notes_sent_count, raw_turns_sent_count, estimated_tokens_full_replay,
		   estimated_tokens_distilled, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.SessionID, e.FromProvider, e.FromModel, e.ToProvider, e.ToModel,
		e.NotesSentCount, e.RawTurnsSentCount, e.EstimatedTokensFullReplay,
		e.EstimatedTokensDistilled, encodeTime(e.CreatedAt))
	if err != nil {
		return fmt.Errorf("storage: recording switch in session %s: %w", e.SessionID, err)
	}
	return nil
}

func (r *switchEventsRepo) ListBySession(ctx context.Context, sessionID string) ([]session.SwitchEvent, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, session_id, from_provider, from_model, to_provider, to_model,
		   notes_sent_count, raw_turns_sent_count, estimated_tokens_full_replay,
		   estimated_tokens_distilled, created_at
		 FROM switch_events WHERE session_id = ? ORDER BY created_at, rowid`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("storage: listing switch events: %w", err)
	}
	defer rows.Close()
	var out []session.SwitchEvent
	for rows.Next() {
		var e session.SwitchEvent
		var created dbTime
		if err := rows.Scan(&e.ID, &e.SessionID, &e.FromProvider, &e.FromModel, &e.ToProvider, &e.ToModel,
			&e.NotesSentCount, &e.RawTurnsSentCount, &e.EstimatedTokensFullReplay,
			&e.EstimatedTokensDistilled, &created); err != nil {
			return nil, fmt.Errorf("storage: listing switch events: %w", err)
		}
		e.CreatedAt = created.Time
		out = append(out, e)
	}
	return out, rows.Err()
}
