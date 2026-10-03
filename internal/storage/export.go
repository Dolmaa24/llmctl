package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Dolmaa24/llmctl/internal/session"
)

// The export types mirror llmctl_API_INTERFACE_CONTRACT.md section 10 field
// for field. It is a public file format that users may script against, so a
// golden-file test pins it: changing a name or shape here is a contract
// change, not a refactor.

type exportDoc struct {
	SessionID    string          `json:"session_id"`
	Title        string          `json:"title"`
	ExportedAt   string          `json:"exported_at"`
	Messages     []exportMessage `json:"messages"`
	Notes        []exportNote    `json:"notes"`
	SwitchEvents []exportSwitch  `json:"switch_events"`
}

type exportMessage struct {
	Role      string  `json:"role"`
	Content   string  `json:"content"`
	Provider  *string `json:"provider"` // null for user messages
	Model     *string `json:"model"`
	CreatedAt string  `json:"created_at"`
}

type exportNote struct {
	Content      string   `json:"content"`
	Provider     string   `json:"provider"`
	Model        string   `json:"model"`
	Tags         []string `json:"tags"`
	LinksTo      *string  `json:"links_to"`
	SupersededBy *string  `json:"superseded_by"`
	CreatedAt    string   `json:"created_at"`
}

type exportEndpoint struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type exportSwitch struct {
	From                      exportEndpoint `json:"from"`
	To                        exportEndpoint `json:"to"`
	EstimatedTokensFullReplay int            `json:"estimated_tokens_full_replay"`
	EstimatedTokensDistilled  int            `json:"estimated_tokens_distilled"`
	CreatedAt                 string         `json:"created_at"`
}

// Snapshot is everything stored about one session.
type Snapshot struct {
	Session      session.Session
	Messages     []session.Message
	Notes        []session.Note // superseded notes included
	SwitchEvents []session.SwitchEvent
}

// LoadSnapshot reads a whole session through the repository interfaces, so it
// works against SQLite and the mocks alike.
func LoadSnapshot(ctx context.Context, sessions SessionsRepo, messages MessagesRepo,
	notes NotesRepo, switches SwitchEventsRepo, sessionID string) (Snapshot, error) {
	s, err := sessions.Get(ctx, sessionID)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Session: *s}
	if snap.Messages, err = messages.ListBySession(ctx, sessionID); err != nil {
		return Snapshot{}, err
	}
	// An export is a record of the session, so replaced notes are kept.
	if snap.Notes, err = notes.ListBySession(ctx, sessionID, true); err != nil {
		return Snapshot{}, err
	}
	if snap.SwitchEvents, err = switches.ListBySession(ctx, sessionID); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// LoadSnapshot reads a whole session from this database.
func (db *DB) LoadSnapshot(ctx context.Context, sessionID string) (Snapshot, error) {
	return LoadSnapshot(ctx, db.Sessions(), db.Messages(), db.Notes(), db.SwitchEvents(), sessionID)
}

// ExportJSON renders a snapshot in the contract section 10 format. exportedAt
// is passed in so the output is reproducible.
func ExportJSON(snap Snapshot, exportedAt time.Time) ([]byte, error) {
	doc := exportDoc{
		SessionID:  snap.Session.ID,
		Title:      snap.Session.Title,
		ExportedAt: rfc3339(exportedAt),
		// Empty lists, never null: a script can always iterate them.
		Messages:     []exportMessage{},
		Notes:        []exportNote{},
		SwitchEvents: []exportSwitch{},
	}
	for _, m := range snap.Messages {
		doc.Messages = append(doc.Messages, exportMessage{
			Role: string(m.Role), Content: m.Content,
			Provider: nullable(m.Provider), Model: nullable(m.Model),
			CreatedAt: rfc3339(m.CreatedAt),
		})
	}
	for _, n := range snap.Notes {
		tags := n.Tags
		if tags == nil {
			tags = []string{}
		}
		doc.Notes = append(doc.Notes, exportNote{
			Content: n.Content, Provider: n.Provider, Model: n.Model, Tags: tags,
			LinksTo: n.LinksTo, SupersededBy: n.SupersededBy,
			CreatedAt: rfc3339(n.CreatedAt),
		})
	}
	for _, e := range snap.SwitchEvents {
		doc.SwitchEvents = append(doc.SwitchEvents, exportSwitch{
			From:                      exportEndpoint{e.FromProvider, e.FromModel},
			To:                        exportEndpoint{e.ToProvider, e.ToModel},
			EstimatedTokensFullReplay: e.EstimatedTokensFullReplay,
			EstimatedTokensDistilled:  e.EstimatedTokensDistilled,
			CreatedAt:                 rfc3339(e.CreatedAt),
		})
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// The file is read by people and scripts, not embedded in HTML, so < > &
	// in code snippets are written as themselves.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("storage: encoding export: %w", err)
	}
	return buf.Bytes(), nil
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
