package storage

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dolmaa24/llmctl/internal/mock"
	"github.com/Dolmaa24/llmctl/internal/session"
)

// repoSet is the four repositories behind one backend.
type repoSet struct {
	sessions SessionsRepo
	messages MessagesRepo
	notes    NotesRepo
	switches SwitchEventsRepo
}

func openDB(t *testing.T, path string) *DB {
	t.Helper()
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func sqliteRepos(t *testing.T) repoSet {
	db := openDB(t, filepath.Join(t.TempDir(), "llmctl.db"))
	return repoSet{db.Sessions(), db.Messages(), db.Notes(), db.SwitchEvents()}
}

func mockRepos(t *testing.T) repoSet {
	return repoSet{mock.NewSessionsRepo(), mock.NewMessagesRepo(), mock.NewNotesRepo(), mock.NewSwitchEventsRepo()}
}

var day = time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)

func newSession(id string) *session.Session {
	return &session.Session{ID: id, Title: "demo", ActiveProvider: "ollama", ActiveModel: "llama3.1:8b",
		CreatedAt: day, UpdatedAt: day}
}

// TestBackendsAgree runs the behaviour every caller relies on against both
// SQLite and the mocks the other modules were built on, so swapping one for
// the other at integration changes nothing they can observe.
func TestBackendsAgree(t *testing.T) {
	backends := map[string]func(*testing.T) repoSet{"sqlite": sqliteRepos, "mock": mockRepos}
	for name, open := range backends {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			r := open(t)

			if err := r.sessions.Create(ctx, newSession("s1")); err != nil {
				t.Fatalf("create session: %v", err)
			}
			later := newSession("s2")
			later.CreatedAt = day.Add(time.Hour)
			if err := r.sessions.Create(ctx, later); err != nil {
				t.Fatalf("create second session: %v", err)
			}
			if err := r.sessions.UpdateActiveProvider(ctx, "s1", "anthropic", "claude-haiku-4-5"); err != nil {
				t.Fatalf("update provider: %v", err)
			}
			got, err := r.sessions.Get(ctx, "s1")
			if err != nil {
				t.Fatalf("get session: %v", err)
			}
			if got.ActiveProvider != "anthropic" || got.ActiveModel != "claude-haiku-4-5" || got.Title != "demo" {
				t.Errorf("session after update = %+v", got)
			}
			if !got.CreatedAt.Equal(day) {
				t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, day)
			}
			list, err := r.sessions.List(ctx)
			if err != nil || len(list) != 2 || list[0].ID != "s1" || list[1].ID != "s2" {
				t.Errorf("List = %+v, %v; want s1 then s2", list, err)
			}
			if _, err := r.sessions.Get(ctx, "missing"); err == nil {
				t.Error("Get of a missing session returned no error")
			}

			// Appended out of order, with the app's zero-based numbering.
			for _, seq := range []int{1, 0, 2} {
				m := &session.Message{ID: "m" + string(rune('0'+seq)), SessionID: "s1", SequenceNum: seq,
					Role: session.RoleUser, Content: "turn", CreatedAt: day}
				if seq == 1 {
					m.Role, m.Provider, m.Model, m.TokenCount = session.RoleAssistant, "ollama", "llama3.1:8b", 42
				}
				if err := r.messages.Append(ctx, m); err != nil {
					t.Fatalf("append message %d: %v", seq, err)
				}
			}
			msgs, err := r.messages.ListBySession(ctx, "s1")
			if err != nil || len(msgs) != 3 {
				t.Fatalf("list messages = %d, %v; want 3", len(msgs), err)
			}
			for i, m := range msgs {
				if m.SequenceNum != i {
					t.Errorf("message %d has SequenceNum %d", i, m.SequenceNum)
				}
			}
			if m := msgs[1]; m.Role != session.RoleAssistant || m.Provider != "ollama" || m.TokenCount != 42 {
				t.Errorf("assistant message = %+v", m)
			}
			if m := msgs[0]; m.Provider != "" || m.Model != "" || m.TokenCount != 0 {
				t.Errorf("user message should carry no attribution, got %+v", m)
			}

			old := &session.Note{ID: "n1", SessionID: "s1", Content: "decided: mutex", Provider: "ollama",
				Model: "llama3.1:8b", Tags: []string{"decision"}, CreatedAt: day}
			replacement := &session.Note{ID: "n2", SessionID: "s1", Content: "decided: atomic CAS", Provider: "ollama",
				Model: "llama3.1:8b", Tags: []string{"decision", "concurrency"}, LinksTo: &old.ID,
				CreatedAt: day.Add(time.Minute)}
			for _, n := range []*session.Note{old, replacement} {
				if err := r.notes.Create(ctx, n); err != nil {
					t.Fatalf("create note %s: %v", n.ID, err)
				}
			}
			if err := r.notes.MarkSuperseded(ctx, "n1", "n2"); err != nil {
				t.Fatalf("mark superseded: %v", err)
			}
			active, err := r.notes.ListBySession(ctx, "s1", false)
			if err != nil || len(active) != 1 || active[0].ID != "n2" {
				t.Fatalf("active notes = %+v, %v; want only n2", active, err)
			}
			if n := active[0]; n.LinksTo == nil || *n.LinksTo != "n1" || len(n.Tags) != 2 || n.Tags[1] != "concurrency" {
				t.Errorf("note n2 = %+v", n)
			}
			all, err := r.notes.ListBySession(ctx, "s1", true)
			if err != nil || len(all) != 2 {
				t.Fatalf("all notes = %d, %v; want 2", len(all), err)
			}
			if all[0].SupersededBy == nil || *all[0].SupersededBy != "n2" {
				t.Errorf("n1.SupersededBy = %v, want n2", all[0].SupersededBy)
			}
			if err := r.notes.MarkSuperseded(ctx, "missing", "n2"); err == nil {
				t.Error("superseding a missing note returned no error")
			}

			event := &session.SwitchEvent{ID: "e1", SessionID: "s1", FromProvider: "ollama", FromModel: "llama3.1:8b",
				ToProvider: "anthropic", ToModel: "claude-haiku-4-5", NotesSentCount: 1, RawTurnsSentCount: 4,
				EstimatedTokensFullReplay: 9000, EstimatedTokensDistilled: 1200, CreatedAt: day}
			if err := r.switches.Create(ctx, event); err != nil {
				t.Fatalf("create switch event: %v", err)
			}
			events, err := r.switches.ListBySession(ctx, "s1")
			if err != nil || len(events) != 1 {
				t.Fatalf("switch events = %d, %v; want 1", len(events), err)
			}
			if got := events[0]; got != *event {
				t.Errorf("switch event = %+v, want %+v", got, *event)
			}
			if other, _ := r.messages.ListBySession(ctx, "s2"); len(other) != 0 {
				t.Errorf("session s2 should have no messages, got %d", len(other))
			}
		})
	}
}

func TestAssignsIDsAndTimesLeftEmpty(t *testing.T) {
	ctx := context.Background()
	r := sqliteRepos(t)

	s := &session.Session{Title: "untitled"}
	if err := r.sessions.Create(ctx, s); err != nil {
		t.Fatalf("create session: %v", err)
	}
	m := &session.Message{SessionID: s.ID, Role: session.RoleUser, Content: "hi"}
	n := &session.Note{SessionID: s.ID, Content: "a fact"}
	e := &session.SwitchEvent{SessionID: s.ID, ToProvider: "ollama", ToModel: "llama3.1:8b"}
	if err := r.messages.Append(ctx, m); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := r.notes.Create(ctx, n); err != nil {
		t.Fatalf("create note: %v", err)
	}
	if err := r.switches.Create(ctx, e); err != nil {
		t.Fatalf("create switch: %v", err)
	}
	for kind, id := range map[string]string{"session": s.ID, "message": m.ID, "note": n.ID, "switch event": e.ID} {
		if len(id) != 36 {
			t.Errorf("%s ID = %q, want a UUID", kind, id)
		}
	}
	if s.CreatedAt.IsZero() || m.CreatedAt.IsZero() || n.CreatedAt.IsZero() || e.CreatedAt.IsZero() {
		t.Error("a CreatedAt was left zero")
	}
	notes, _ := r.notes.ListBySession(ctx, s.ID, true)
	if len(notes) != 1 || notes[0].Tags == nil || len(notes[0].Tags) != 0 {
		t.Errorf("a note stored without tags should read back with an empty list, got %+v", notes)
	}
}

func TestMissingRowsReturnErrNotFound(t *testing.T) {
	ctx := context.Background()
	r := sqliteRepos(t)
	if err := r.sessions.Create(ctx, newSession("s1")); err != nil {
		t.Fatal(err)
	}
	if err := r.notes.Create(ctx, &session.Note{ID: "n1", SessionID: "s1", Content: "x"}); err != nil {
		t.Fatal(err)
	}

	_, getErr := r.sessions.Get(ctx, "nope")
	cases := map[string]error{
		"get session":         getErr,
		"update session":      r.sessions.UpdateActiveProvider(ctx, "nope", "ollama", "m"),
		"append to session":   r.messages.Append(ctx, &session.Message{SessionID: "nope", Role: session.RoleUser}),
		"supersede missing":   r.notes.MarkSuperseded(ctx, "nope", "n1"),
		"supersede by absent": r.notes.MarkSuperseded(ctx, "n1", "nope"),
	}
	for name, err := range cases {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: got %v, want ErrNotFound", name, err)
		}
	}
	// A foreign-key failure is a database error, not a crash.
	if err := r.notes.Create(ctx, &session.Note{SessionID: "nope", Content: "x"}); err == nil {
		t.Error("a note for an unknown session was accepted")
	}
	if err := r.switches.Create(ctx, &session.SwitchEvent{SessionID: "nope", ToProvider: "p", ToModel: "m"}); err == nil {
		t.Error("a switch event for an unknown session was accepted")
	}
}

func TestRejectsInvalidInput(t *testing.T) {
	ctx := context.Background()
	r := sqliteRepos(t)
	if err := r.sessions.Create(ctx, newSession("s1")); err != nil {
		t.Fatal(err)
	}
	self := "n-self"
	cases := map[string]error{
		"nil session":         r.sessions.Create(ctx, nil),
		"id with a space":     r.sessions.Create(ctx, &session.Session{ID: "bad id"}),
		"overlong id":         r.sessions.Create(ctx, &session.Session{ID: strings.Repeat("a", maxIDBytes+1)}),
		"control in title":    r.sessions.Create(ctx, &session.Session{Title: "a\x1b[2Jb"}),
		"unknown role":        r.messages.Append(ctx, &session.Message{SessionID: "s1", Role: "robot"}),
		"negative sequence":   r.messages.Append(ctx, &session.Message{SessionID: "s1", Role: session.RoleUser, SequenceNum: -1}),
		"invalid UTF-8":       r.messages.Append(ctx, &session.Message{SessionID: "s1", Role: session.RoleUser, Content: "\xff"}),
		"blank note":          r.notes.Create(ctx, &session.Note{SessionID: "s1", Content: "  "}),
		"empty tag":           r.notes.Create(ctx, &session.Note{SessionID: "s1", Content: "x", Tags: []string{""}}),
		"note links itself":   r.notes.Create(ctx, &session.Note{ID: self, SessionID: "s1", Content: "x", LinksTo: &self}),
		"supersede itself":    r.notes.MarkSuperseded(ctx, "n1", "n1"),
		"switch to nothing":   r.switches.Create(ctx, &session.SwitchEvent{SessionID: "s1"}),
		"negative estimate":   r.switches.Create(ctx, &session.SwitchEvent{SessionID: "s1", ToProvider: "p", ToModel: "m", EstimatedTokensDistilled: -1}),
		"empty database path": func() error { _, err := Open(ctx, ""); return err }(),
	}
	for name, err := range cases {
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", name, err)
		}
	}
}

func TestDuplicateSequenceNumberIsRejected(t *testing.T) {
	ctx := context.Background()
	r := sqliteRepos(t)
	if err := r.sessions.Create(ctx, newSession("s1")); err != nil {
		t.Fatal(err)
	}
	first := &session.Message{SessionID: "s1", SequenceNum: 0, Role: session.RoleUser, Content: "a"}
	if err := r.messages.Append(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := r.messages.Append(ctx, &session.Message{SessionID: "s1", SequenceNum: 0, Role: session.RoleUser, Content: "b"}); err == nil {
		t.Fatal("a second message with sequence number 0 was accepted")
	}
	// The rejected append must leave nothing behind.
	if msgs, _ := r.messages.ListBySession(ctx, "s1"); len(msgs) != 1 || msgs[0].Content != "a" {
		t.Errorf("messages after rejected append = %+v", msgs)
	}
}

func TestSupersedeLoopIsRejected(t *testing.T) {
	ctx := context.Background()
	r := sqliteRepos(t)
	for _, id := range []string{"s1", "s2"} {
		if err := r.sessions.Create(ctx, newSession(id)); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []session.Note{
		{ID: "a", SessionID: "s1", Content: "a"}, {ID: "b", SessionID: "s1", Content: "b"},
		{ID: "c", SessionID: "s1", Content: "c"}, {ID: "other", SessionID: "s2", Content: "x"},
	} {
		if err := r.notes.Create(ctx, &n); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.notes.MarkSuperseded(ctx, "a", "b"); err != nil {
		t.Fatalf("a <- b: %v", err)
	}
	if err := r.notes.MarkSuperseded(ctx, "b", "c"); err != nil {
		t.Fatalf("b <- c: %v", err)
	}
	if err := r.notes.MarkSuperseded(ctx, "c", "a"); !errors.Is(err, ErrInvalid) {
		t.Errorf("closing the loop c <- a: got %v, want ErrInvalid", err)
	}
	if err := r.notes.MarkSuperseded(ctx, "c", "other"); !errors.Is(err, ErrInvalid) {
		t.Errorf("superseding across sessions: got %v, want ErrInvalid", err)
	}
	active, err := r.notes.ListBySession(ctx, "s1", false)
	if err != nil || len(active) != 1 || active[0].ID != "c" {
		t.Errorf("active notes = %+v, %v; want only c", active, err)
	}
}

// TestWritesSurviveReopen is the NFR-6 check: every call has committed by the
// time it returns, so closing without any flush step loses nothing.
func TestWritesSurviveReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "llmctl.db")

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := db.Sessions().Create(ctx, newSession("s1")); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 10, 4, 9, 30, 15, 123456789, time.FixedZone("IST", 5*3600+1800))
	if err := db.Messages().Append(ctx, &session.Message{SessionID: "s1", Role: session.RoleUser,
		Content: "línea one\nline two", CreatedAt: stamp}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	again := openDB(t, path)
	msgs, err := again.Messages().ListBySession(ctx, "s1")
	if err != nil || len(msgs) != 1 {
		t.Fatalf("after reopen: %d messages, %v", len(msgs), err)
	}
	if msgs[0].Content != "línea one\nline two" {
		t.Errorf("content = %q", msgs[0].Content)
	}
	if !msgs[0].CreatedAt.Equal(stamp) || msgs[0].CreatedAt.Location() != time.UTC {
		t.Errorf("CreatedAt = %v, want %v in UTC", msgs[0].CreatedAt, stamp.UTC())
	}
	s, err := again.Sessions().Get(ctx, "s1")
	if err != nil || !s.UpdatedAt.Equal(stamp) {
		t.Errorf("session UpdatedAt = %v, %v; want the message time %v", s, err, stamp.UTC())
	}
}

func TestDeletingASessionRemovesItsRows(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "llmctl.db"))
	if err := db.Sessions().Create(ctx, newSession("s1")); err != nil {
		t.Fatal(err)
	}
	if err := db.Notes().Create(ctx, &session.Note{SessionID: "s1", Content: "x"}); err != nil {
		t.Fatal(err)
	}
	// Foreign keys must be on for the connection Open hands out, not just
	// for the one that ran the migrations.
	if _, err := db.sql.ExecContext(ctx, `DELETE FROM sessions WHERE id = 's1'`); err != nil {
		t.Fatal(err)
	}
	if notes, _ := db.Notes().ListBySession(ctx, "s1", true); len(notes) != 0 {
		t.Errorf("notes left after their session was deleted: %d", len(notes))
	}
}
