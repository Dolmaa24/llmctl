package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Dolmaa24/llmctl/internal/session"
)

var _ NotesRepo = (*notesRepo)(nil)

type notesRepo struct{ db *sql.DB }

func (r *notesRepo) Create(ctx context.Context, n *session.Note) error {
	if n == nil {
		return fmt.Errorf("%w: note is nil", ErrInvalid)
	}
	if err := ensureID("note id", &n.ID); err != nil {
		return err
	}
	if strings.TrimSpace(n.Content) == "" {
		return fmt.Errorf("%w: note content is empty", ErrInvalid)
	}
	if err := checkContent("content", n.Content); err != nil {
		return err
	}
	if err := checkProviderModel("", n.Provider, n.Model); err != nil {
		return err
	}
	if len(n.Tags) > maxTags {
		return fmt.Errorf("%w: a note may carry at most %d tags", ErrInvalid, maxTags)
	}
	for _, tag := range n.Tags {
		if tag == "" {
			return fmt.Errorf("%w: tags must not be empty", ErrInvalid)
		}
		if err := checkName("tag", tag, maxTagBytes); err != nil {
			return err
		}
	}
	for field, ref := range map[string]*string{"links_to": n.LinksTo, "superseded_by": n.SupersededBy} {
		if ref == nil {
			continue
		}
		if err := checkID(field, *ref); err != nil {
			return err
		}
		if *ref == n.ID {
			return fmt.Errorf("%w: a note cannot refer to itself in %s", ErrInvalid, field)
		}
	}
	tags := n.Tags
	if tags == nil {
		tags = []string{}
	}
	tagJSON, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("storage: encoding tags: %w", err)
	}
	n.CreatedAt = orNow(n.CreatedAt)
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO notes (id, session_id, content, provider, model, tags, links_to, superseded_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.SessionID, n.Content, n.Provider, n.Model, string(tagJSON),
		n.LinksTo, n.SupersededBy, encodeTime(n.CreatedAt))
	if err != nil {
		return fmt.Errorf("storage: creating note in session %s: %w", n.SessionID, err)
	}
	return nil
}

// ListBySession returns notes oldest first. With includeSuperseded false it
// returns only the notes a handoff should carry.
func (r *notesRepo) ListBySession(ctx context.Context, sessionID string, includeSuperseded bool) ([]session.Note, error) {
	query := `SELECT id, session_id, content, provider, model, tags, links_to, superseded_by, created_at
	          FROM notes WHERE session_id = ?`
	if !includeSuperseded {
		query += ` AND superseded_by IS NULL`
	}
	query += ` ORDER BY created_at, rowid`
	rows, err := r.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("storage: listing notes: %w", err)
	}
	defer rows.Close()
	var out []session.Note
	for rows.Next() {
		var n session.Note
		var tags string
		var linksTo, supersededBy sql.NullString
		var created dbTime
		if err := rows.Scan(&n.ID, &n.SessionID, &n.Content, &n.Provider, &n.Model,
			&tags, &linksTo, &supersededBy, &created); err != nil {
			return nil, fmt.Errorf("storage: listing notes: %w", err)
		}
		if err := json.Unmarshal([]byte(tags), &n.Tags); err != nil {
			return nil, fmt.Errorf("storage: note %s has unreadable tags: %w", n.ID, err)
		}
		if linksTo.Valid {
			n.LinksTo = &linksTo.String
		}
		if supersededBy.Valid {
			n.SupersededBy = &supersededBy.String
		}
		n.CreatedAt = created.Time
		out = append(out, n)
	}
	return out, rows.Err()
}

// MarkSuperseded records that supersededByID replaces noteID. Both notes must
// exist in the same session, and the replacement must not itself lead back to
// noteID: a loop would leave every note in it superseded, and the decision
// they describe would silently drop out of every handoff.
func (r *notesRepo) MarkSuperseded(ctx context.Context, noteID, supersededByID string) error {
	if noteID == supersededByID {
		return fmt.Errorf("%w: note %s cannot supersede itself", ErrInvalid, noteID)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: superseding note: %w", err)
	}
	defer tx.Rollback()

	oldSession, _, err := noteLink(ctx, tx, noteID)
	if err != nil {
		return err
	}
	newSession, next, err := noteLink(ctx, tx, supersededByID)
	if err != nil {
		return err
	}
	if oldSession != newSession {
		return fmt.Errorf("%w: notes %s and %s belong to different sessions", ErrInvalid, noteID, supersededByID)
	}
	// Follow the replacement's own chain. The chain is finite because this
	// check has kept it loop-free on every earlier call.
	for next != "" {
		if next == noteID {
			return fmt.Errorf("%w: superseding %s with %s would form a loop", ErrInvalid, noteID, supersededByID)
		}
		if _, next, err = noteLink(ctx, tx, next); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notes SET superseded_by = ? WHERE id = ?`,
		supersededByID, noteID); err != nil {
		return fmt.Errorf("storage: superseding note %s: %w", noteID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: superseding note: %w", err)
	}
	return nil
}

// noteLink returns a note's session and the note that supersedes it, if any.
func noteLink(ctx context.Context, tx *sql.Tx, id string) (sessionID, supersededBy string, err error) {
	var by sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT session_id, superseded_by FROM notes WHERE id = ?`, id).
		Scan(&sessionID, &by)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", fmt.Errorf("%w: note %q", ErrNotFound, id)
	}
	if err != nil {
		return "", "", fmt.Errorf("storage: reading note %s: %w", id, err)
	}
	return sessionID, by.String, nil
}
