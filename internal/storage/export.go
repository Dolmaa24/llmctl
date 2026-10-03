package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// ExportMarkdown renders a snapshot as a readable transcript followed by the
// notes (SRS FR-5.3). Each assistant turn is attributed to its provider and
// model, and each switch appears as a divider where it happened, as in the
// TUI transcript. Unlike the JSON export this is for people, and its layout
// is not a contract.
func ExportMarkdown(snap Snapshot, exportedAt time.Time) []byte {
	var b strings.Builder
	title := snap.Session.Title
	if title == "" {
		title = "Untitled session"
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	fmt.Fprintf(&b, "- Session: `%s`\n- Exported: %s\n\n", snap.Session.ID, rfc3339(exportedAt))

	b.WriteString("## Transcript\n\n")
	if len(snap.Messages) == 0 {
		b.WriteString("_No messages._\n\n")
	}
	next := 0
	divider := func(e session.SwitchEvent) {
		fmt.Fprintf(&b, "---\n\n*Switched to **%s/%s**: distilled handoff about %d tokens, full replay about %d*\n\n---\n\n",
			e.ToProvider, e.ToModel, e.EstimatedTokensDistilled, e.EstimatedTokensFullReplay)
	}
	for _, m := range snap.Messages {
		for next < len(snap.SwitchEvents) && !snap.SwitchEvents[next].CreatedAt.After(m.CreatedAt) {
			divider(snap.SwitchEvents[next])
			next++
		}
		who := "**You**"
		switch m.Role {
		case session.RoleAssistant:
			who = fmt.Sprintf("**%s/%s**", m.Provider, m.Model)
		case session.RoleSystem:
			who = "**System**"
		}
		fmt.Fprintf(&b, "%s · %s\n\n%s\n\n", who, rfc3339(m.CreatedAt), m.Content)
	}
	// A switch after the last message still happened and is still shown.
	for ; next < len(snap.SwitchEvents); next++ {
		divider(snap.SwitchEvents[next])
	}

	b.WriteString("## Notes\n\n")
	if len(snap.Notes) == 0 {
		b.WriteString("_No notes extracted._\n")
	}
	for _, n := range snap.Notes {
		line := fmt.Sprintf("%s (*%s/%s*)", n.Content, n.Provider, n.Model)
		for _, tag := range n.Tags {
			line += " `#" + tag + "`"
		}
		if n.SupersededBy != nil {
			line = "~~" + line + "~~ (superseded)"
		}
		b.WriteString("- " + line + "\n")
	}
	return []byte(b.String())
}
