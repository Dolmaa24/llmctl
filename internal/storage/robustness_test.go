package storage

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dolmaa24/llmctl/internal/session"
)

// These are the storage error paths of the build plan's Phase 3: a damaged
// database, an abrupt exit, and the cascade rules the schema promises.

func TestDamagedDatabaseIsAnErrorNotACrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llmctl.db")
	if err := os.WriteFile(path, bytes.Repeat([]byte("this is not a sqlite file\n"), 200), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := Open(context.Background(), path)
	if err == nil {
		db.Close()
		t.Fatal("Open accepted a file that is not a database")
	}
	// The damaged file is left as it was, for the user to recover or remove.
	if data, _ := os.ReadFile(path); !bytes.HasPrefix(data, []byte("this is not")) {
		t.Error("Open modified the damaged file")
	}
}

func TestOpenFailsCleanlyWhenTheDirectoryCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if db, err := Open(context.Background(), filepath.Join(blocker, "llmctl.db")); err == nil {
		db.Close()
		t.Fatal("Open succeeded under a path whose parent is a file")
	}
}

func TestCancelledContextIsReturnedAsAnError(t *testing.T) {
	r := sqliteRepos(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.sessions.Create(ctx, newSession("s1")); err == nil {
		t.Error("Create with a cancelled context returned no error")
	}
	if _, err := r.sessions.List(ctx); err == nil {
		t.Error("List with a cancelled context returned no error")
	}
}

var crashDB = flag.String("crash-db", "", "internal: database path for the crash helper")

// TestCrashHelper is not a test. TestNothingIsLostOnAnAbruptExit re-runs the
// test binary with -crash-db so that a separate process writes to the
// database and then dies without closing it.
func TestCrashHelper(t *testing.T) {
	if *crashDB == "" {
		t.Skip("helper for TestNothingIsLostOnAnAbruptExit")
	}
	ctx := context.Background()
	db, err := Open(ctx, *crashDB)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := db.Sessions().Create(ctx, newSession("s1")); err != nil {
		os.Exit(2)
	}
	for i := 0; i < 25; i++ {
		err := db.Messages().Append(ctx, &session.Message{SessionID: "s1", SequenceNum: i,
			Role: session.RoleUser, Content: fmt.Sprintf("turn %d", i)})
		if err != nil {
			os.Exit(2)
		}
		if i%5 == 0 {
			if err := db.Notes().Create(ctx, &session.Note{SessionID: "s1", Content: fmt.Sprintf("fact %d", i)}); err != nil {
				os.Exit(2)
			}
		}
	}
	// No Close, no flush, no deferred functions: the process just stops.
	os.Exit(7)
}

// TestNothingIsLostOnAnAbruptExit is the SRS NFR-6 check. Every write the
// helper process saw return must be on disk after it dies.
func TestNothingIsLostOnAnAbruptExit(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a subprocess")
	}
	path := filepath.Join(t.TempDir(), "llmctl.db")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$", "-crash-db="+path)
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 7 {
		t.Fatalf("helper did not exit the way it should: %v\n%s", err, out)
	}

	db := openDB(t, path)
	ctx := context.Background()
	msgs, err := db.Messages().ListBySession(ctx, "s1")
	if err != nil {
		t.Fatalf("reading after the crash: %v", err)
	}
	if len(msgs) != 25 || msgs[24].Content != "turn 24" {
		t.Errorf("%d messages survived, want all 25", len(msgs))
	}
	notes, err := db.Notes().ListBySession(ctx, "s1", true)
	if err != nil || len(notes) != 5 {
		t.Errorf("%d notes survived (err %v), want all 5", len(notes), err)
	}
	// And the database is still writable afterwards.
	if err := db.Messages().Append(ctx, &session.Message{SessionID: "s1", SequenceNum: 25,
		Role: session.RoleUser, Content: "after restart"}); err != nil {
		t.Errorf("append after the crash: %v", err)
	}
}

func TestDeletingASessionWithLinkedNotes(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "llmctl.db"))
	seedExportSession(t, repoSet{db.Sessions(), db.Messages(), db.Notes(), db.SwitchEvents()})
	if err := db.Sessions().Create(ctx, newSession("keep")); err != nil {
		t.Fatal(err)
	}
	if err := db.Notes().Create(ctx, &session.Note{ID: "k1", SessionID: "keep", Content: "stays"}); err != nil {
		t.Fatal(err)
	}

	// s1 holds notes that link to and supersede one another.
	if _, err := db.sql.ExecContext(ctx, `DELETE FROM sessions WHERE id = 's1'`); err != nil {
		t.Fatalf("deleting a session whose notes reference each other: %v", err)
	}
	for _, table := range []string{"messages", "notes", "switch_events"} {
		var n int
		if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE session_id = 's1'`).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s still holds %d rows of the deleted session (err %v)", table, n, err)
		}
	}
	if kept, _ := db.Notes().ListBySession(ctx, "keep", true); len(kept) != 1 {
		t.Errorf("the other session lost its notes: %+v", kept)
	}
}

func TestProvidersMirror(t *testing.T) {
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "llmctl.db"))

	ollama := ProviderRow{ID: "ollama", DisplayName: "Ollama", BaseURL: "http://127.0.0.1:11434",
		DefaultModel: "llama3.1:8b", Enabled: true}
	for _, p := range []ProviderRow{ollama, {ID: "anthropic", DisplayName: "Anthropic"}} {
		if err := db.UpsertProvider(ctx, p); err != nil {
			t.Fatalf("upsert %s: %v", p.ID, err)
		}
	}
	ollama.DefaultModel, ollama.Enabled = "qwen2.5:7b", false
	if err := db.UpsertProvider(ctx, ollama); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	got, err := db.Providers(ctx)
	if err != nil || len(got) != 2 || got[0].ID != "anthropic" || got[1] != ollama {
		t.Fatalf("Providers = %+v, %v", got, err)
	}

	if err := db.DeleteProvider(ctx, "anthropic"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteProvider(ctx, "anthropic"); err != nil {
		t.Errorf("deleting an absent provider: %v", err)
	}
	if got, _ := db.Providers(ctx); len(got) != 1 {
		t.Errorf("after delete: %+v", got)
	}
	if err := db.UpsertProvider(ctx, ProviderRow{ID: "bad id"}); err == nil {
		t.Error("a malformed provider ID was accepted")
	}
	// The table has no column a key could go in.
	var columns string
	rows, err := db.sql.QueryContext(ctx, `SELECT name FROM pragma_table_info('providers')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		rows.Scan(&name)
		columns += name + " "
	}
	if strings.Contains(columns, "key") || strings.Contains(columns, "secret") {
		t.Errorf("providers table has a secret-looking column: %s", columns)
	}
}

func TestMarkdownExport(t *testing.T) {
	r := sqliteRepos(t)
	seedExportSession(t, r)
	snap, err := LoadSnapshot(context.Background(), r.sessions, r.messages, r.notes, r.switches, "s1")
	if err != nil {
		t.Fatal(err)
	}
	md := string(ExportMarkdown(snap, day.Add(time.Hour)))
	for _, want := range []string{
		"# Rate limiter design",
		"**You** · 2026-10-04T09:00:00Z",
		"**ollama/llama3.1:8b** · 2026-10-04T09:00:05Z",
		"*Switched to **anthropic/claude-haiku-4-5**: distilled handoff about 1200 tokens, full replay about 9000*",
		"- ~~decided: mutex for refill timer (*ollama/llama3.1:8b*) `#decision`~~ (superseded)",
		"- decided: atomic CAS for refill timer (*ollama/llama3.1:8b*) `#decision` `#concurrency`",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown export is missing %q\n%s", want, md)
		}
	}
	empty := string(ExportMarkdown(Snapshot{Session: session.Session{ID: "s"}}, day))
	if !strings.Contains(empty, "# Untitled session") || !strings.Contains(empty, "_No notes extracted._") {
		t.Errorf("empty session export:\n%s", empty)
	}
}
