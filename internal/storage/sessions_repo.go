package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Dolmaa24/llmctl/internal/session"
)

// Every method commits before it returns. Nothing is held in memory until
// exit, so an unexpected termination loses at most the write in flight
// (SRS NFR-6).

var _ SessionsRepo = (*sessionsRepo)(nil)

type sessionsRepo struct{ db *sql.DB }

func (r *sessionsRepo) Create(ctx context.Context, s *session.Session) error {
	if s == nil {
		return fmt.Errorf("%w: session is nil", ErrInvalid)
	}
	if err := ensureID("session id", &s.ID); err != nil {
		return err
	}
	if err := checkName("title", s.Title, maxTitleBytes); err != nil {
		return err
	}
	if err := checkProviderModel("active ", s.ActiveProvider, s.ActiveModel); err != nil {
		return err
	}
	s.CreatedAt = orNow(s.CreatedAt)
	s.UpdatedAt = orNow(s.UpdatedAt)
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO sessions (id, title, active_provider, active_model, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		s.ID, s.Title, s.ActiveProvider, s.ActiveModel, encodeTime(s.CreatedAt), encodeTime(s.UpdatedAt))
	if err != nil {
		return fmt.Errorf("storage: creating session %s: %w", s.ID, err)
	}
	return nil
}

func (r *sessionsRepo) Get(ctx context.Context, id string) (*session.Session, error) {
	var s session.Session
	var created, updated dbTime
	err := r.db.QueryRowContext(ctx,
		`SELECT id, title, active_provider, active_model, created_at, updated_at
		 FROM sessions WHERE id = ?`, id,
	).Scan(&s.ID, &s.Title, &s.ActiveProvider, &s.ActiveModel, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: session %q", ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: reading session %s: %w", id, err)
	}
	s.CreatedAt, s.UpdatedAt = created.Time, updated.Time
	return &s, nil
}

func (r *sessionsRepo) UpdateActiveProvider(ctx context.Context, id, provider, model string) error {
	if err := checkProviderModel("active ", provider, model); err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE sessions SET active_provider = ?, active_model = ?, updated_at = ? WHERE id = ?`,
		provider, model, encodeTime(time.Now()), id)
	if err != nil {
		return fmt.Errorf("storage: updating session %s: %w", id, err)
	}
	return mustAffect(res, "session", id)
}

// List returns sessions oldest first, as the mock does.
func (r *sessionsRepo) List(ctx context.Context) ([]session.Session, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, title, active_provider, active_model, created_at, updated_at
		 FROM sessions ORDER BY created_at, rowid`)
	if err != nil {
		return nil, fmt.Errorf("storage: listing sessions: %w", err)
	}
	defer rows.Close()
	var out []session.Session
	for rows.Next() {
		var s session.Session
		var created, updated dbTime
		if err := rows.Scan(&s.ID, &s.Title, &s.ActiveProvider, &s.ActiveModel, &created, &updated); err != nil {
			return nil, fmt.Errorf("storage: listing sessions: %w", err)
		}
		s.CreatedAt, s.UpdatedAt = created.Time, updated.Time
		out = append(out, s)
	}
	return out, rows.Err()
}
