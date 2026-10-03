package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Dolmaa24/llmctl/internal/session"
)

// Every method commits before it returns. Nothing is held in memory until
// exit, so an unexpected termination loses at most the write in flight
// (SRS NFR-6).

var (
	_ SessionsRepo     = (*sessionsRepo)(nil)
	_ MessagesRepo     = (*messagesRepo)(nil)
	_ NotesRepo        = (*notesRepo)(nil)
	_ SwitchEventsRepo = (*switchEventsRepo)(nil)
)

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

type messagesRepo struct{ db *sql.DB }

// Append stores one message. SequenceNum is the caller's: the app numbers
// turns from zero, so zero cannot mean "unset" here. A duplicate number in
// the same session is rejected by the schema.
func (r *messagesRepo) Append(ctx context.Context, m *session.Message) error {
	if m == nil {
		return fmt.Errorf("%w: message is nil", ErrInvalid)
	}
	if err := ensureID("message id", &m.ID); err != nil {
		return err
	}
	switch m.Role {
	case session.RoleUser, session.RoleAssistant, session.RoleSystem:
	default:
		return fmt.Errorf("%w: role must be user, assistant or system", ErrInvalid)
	}
	if err := checkContent("content", m.Content); err != nil {
		return err
	}
	if err := checkProviderModel("", m.Provider, m.Model); err != nil {
		return err
	}
	if err := checkCount("sequence number", m.SequenceNum); err != nil {
		return err
	}
	if err := checkCount("token count", m.TokenCount); err != nil {
		return err
	}
	m.CreatedAt = orNow(m.CreatedAt)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: appending message: %w", err)
	}
	defer tx.Rollback()
	// Touching the session first both records activity and proves it exists.
	res, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at = ? WHERE id = ?`,
		encodeTime(m.CreatedAt), m.SessionID)
	if err != nil {
		return fmt.Errorf("storage: appending message: %w", err)
	}
	if err := mustAffect(res, "session", m.SessionID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO messages (id, session_id, sequence_num, role, content, provider, model, token_count, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.SessionID, m.SequenceNum, string(m.Role), m.Content,
		nullIfEmpty(m.Provider), nullIfEmpty(m.Model), nullIfZero(m.TokenCount), encodeTime(m.CreatedAt))
	if err != nil {
		return fmt.Errorf("storage: appending message %d of session %s: %w", m.SequenceNum, m.SessionID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: appending message: %w", err)
	}
	return nil
}

func (r *messagesRepo) ListBySession(ctx context.Context, sessionID string) ([]session.Message, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, session_id, sequence_num, role, content, provider, model, token_count, created_at
		 FROM messages WHERE session_id = ? ORDER BY sequence_num`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("storage: listing messages: %w", err)
	}
	defer rows.Close()
	var out []session.Message
	for rows.Next() {
		var m session.Message
		var role string
		var provider, model sql.NullString
		var tokens sql.NullInt64
		var created dbTime
		if err := rows.Scan(&m.ID, &m.SessionID, &m.SequenceNum, &role, &m.Content,
			&provider, &model, &tokens, &created); err != nil {
			return nil, fmt.Errorf("storage: listing messages: %w", err)
		}
		m.Role = session.Role(role)
		m.Provider, m.Model, m.TokenCount = provider.String, model.String, int(tokens.Int64)
		m.CreatedAt = created.Time
		out = append(out, m)
	}
	return out, rows.Err()
}

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

func mustAffect(res sql.Result, kind, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s %q", ErrNotFound, kind, id)
	}
	return nil
}

// The schema keeps provider, model and token_count null for user messages
// and for counts not yet estimated.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIfZero(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
