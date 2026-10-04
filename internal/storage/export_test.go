package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dolmaa24/llmctl/internal/session"
)

var update = flag.Bool("update", false, "rewrite the golden export file")

// seedExportSession stores one session that exercises every export field:
// a user message with no attribution, a superseded note, a linked note, a
// note with no tags, and a switch.
func seedExportSession(t *testing.T, r repoSet) {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(r.sessions.Create(ctx, &session.Session{ID: "s1", Title: "Rate limiter design",
		ActiveProvider: "ollama", ActiveModel: "llama3.1:8b", CreatedAt: day, UpdatedAt: day}))
	must(r.messages.Append(ctx, &session.Message{ID: "m0", SessionID: "s1", SequenceNum: 0,
		Role: session.RoleUser, Content: "How should the refill timer work?", CreatedAt: day}))
	must(r.messages.Append(ctx, &session.Message{ID: "m1", SessionID: "s1", SequenceNum: 1,
		Role: session.RoleAssistant, Content: "Use an atomic compare-and-swap.\nNo mutex needed.",
		Provider: "ollama", Model: "llama3.1:8b", TokenCount: 12, CreatedAt: day.Add(5 * time.Second)}))

	n1, n2 := "n1", "n2"
	must(r.notes.Create(ctx, &session.Note{ID: n1, SessionID: "s1", Content: "decided: mutex for refill timer",
		Provider: "ollama", Model: "llama3.1:8b", Tags: []string{"decision"}, CreatedAt: day.Add(time.Minute)}))
	must(r.notes.Create(ctx, &session.Note{ID: n2, SessionID: "s1", Content: "decided: atomic CAS for refill timer",
		Provider: "ollama", Model: "llama3.1:8b", Tags: []string{"decision", "concurrency"}, LinksTo: &n1,
		CreatedAt: day.Add(2 * time.Minute)}))
	must(r.notes.Create(ctx, &session.Note{ID: "n3", SessionID: "s1", Content: "open: burst size <unset>",
		Provider: "ollama", Model: "llama3.1:8b", CreatedAt: day.Add(3 * time.Minute)}))
	must(r.notes.MarkSuperseded(ctx, n1, n2))

	must(r.switches.Create(ctx, &session.SwitchEvent{ID: "e1", SessionID: "s1",
		FromProvider: "ollama", FromModel: "llama3.1:8b", ToProvider: "anthropic", ToModel: "claude-haiku-4-5",
		NotesSentCount: 2, RawTurnsSentCount: 2, EstimatedTokensFullReplay: 9000, EstimatedTokensDistilled: 1200,
		CreatedAt: day.Add(4 * time.Minute)}))
}

func exportOf(t *testing.T, r repoSet, sessionID string) []byte {
	t.Helper()
	snap, err := LoadSnapshot(context.Background(), r.sessions, r.messages, r.notes, r.switches, sessionID)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	out, err := ExportJSON(snap, day.Add(time.Hour))
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	return out
}

// TestExportMatchesGolden pins the contract section 10 format. If this fails
// after an intended format change, the contract document changes with it;
// then run: go test ./internal/storage -run Golden -update
func TestExportMatchesGolden(t *testing.T) {
	r := sqliteRepos(t)
	seedExportSession(t, r)
	got := exportOf(t, r, "s1")

	golden := filepath.Join("testdata", "export.golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading golden file (run with -update to create it): %v", err)
	}
	// Git may check the golden file out with CRLF line endings on Windows.
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Errorf("export differs from %s\n--- got ---\n%s\n--- want ---\n%s", golden, got, want)
	}
}

func TestExportIsTheSameFromSQLiteAndMocks(t *testing.T) {
	sqlite, mocks := sqliteRepos(t), mockRepos(t)
	seedExportSession(t, sqlite)
	seedExportSession(t, mocks)
	if a, b := exportOf(t, sqlite, "s1"), exportOf(t, mocks, "s1"); !bytes.Equal(a, b) {
		t.Errorf("exports differ\n--- sqlite ---\n%s\n--- mock ---\n%s", a, b)
	}
}

func TestExportOfAnEmptySessionHasEmptyLists(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "llmctl.db"))
	if err := db.Sessions().Create(ctx, newSession("s1")); err != nil {
		t.Fatal(err)
	}
	snap, err := db.LoadSnapshot(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	out, err := ExportJSON(snap, day)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("export is not valid JSON: %v", err)
	}
	for _, key := range []string{"messages", "notes", "switch_events"} {
		if list, ok := doc[key].([]any); !ok || len(list) != 0 {
			t.Errorf("%s = %v, want an empty list", key, doc[key])
		}
	}
	if _, err := db.LoadSnapshot(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("snapshot of a missing session: got %v, want ErrNotFound", err)
	}
}
