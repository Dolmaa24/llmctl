package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Dolmaa24/llmctl/internal/session"
)

var _ MessagesRepo = (*messagesRepo)(nil)

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
