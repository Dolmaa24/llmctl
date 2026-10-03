package storage

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Dolmaa24/llmctl/internal/session"
)

func runsFixture(t *testing.T) (*DB, context.Context) {
	t.Helper()
	ctx := context.Background()
	db := openDB(t, filepath.Join(t.TempDir(), "llmctl.db"))
	if err := db.Sessions().Create(ctx, newSession("s1")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"n1", "n2", "n3"} {
		if err := db.Notes().Create(ctx, &session.Note{ID: id, SessionID: "s1", Content: "fact " + id}); err != nil {
			t.Fatal(err)
		}
	}
	return db, ctx
}

func newRun(through int, at time.Duration, noteIDs ...string) *session.ExtractionRun {
	return &session.ExtractionRun{SessionID: "s1", ThroughSequenceNum: through, Provider: "ollama",
		Model: "qwen2.5:7b", PromptVersion: "v1", InputTokens: 800, OutputTokens: 120,
		NoteIDs: noteIDs, CreatedAt: day.Add(at)}
}

func TestExtractionRunsRoundTrip(t *testing.T) {
	db, ctx := runsFixture(t)
	runs := db.ExtractionRuns()

	first := newRun(3, time.Minute, "n2", "n1")
	failed := newRun(7, 2*time.Minute)
	failed.Err, failed.OutputTokens = "model returned malformed JSON", 0
	third := newRun(7, 3*time.Minute, "n3")
	// Created out of order: the list must still come back oldest first.
	for _, run := range []*session.ExtractionRun{third, first, failed} {
		if err := runs.Create(ctx, run); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if len(run.ID) != 36 {
			t.Errorf("run ID = %q, want an assigned UUID", run.ID)
		}
	}

	got, err := runs.ListBySession(ctx, "s1")
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	want := []session.ExtractionRun{*first, *failed, *third}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("runs = %+v\nwant   %+v", got, want)
	}
	if other, err := runs.ListBySession(ctx, "s2"); err != nil || len(other) != 0 {
		t.Errorf("runs of another session = %+v, %v", other, err)
	}
}

// The two questions the table exists to answer, from the proposal in
// plan/pr-drafts/PR4-extraction-runs.md.
func TestExtractionRunQueries(t *testing.T) {
	db, ctx := runsFixture(t)
	failed := newRun(9, 2*time.Minute)
	failed.Err = "timeout"
	for _, run := range []*session.ExtractionRun{newRun(3, time.Minute, "n1"), failed, newRun(6, 3*time.Minute, "n2")} {
		if err := db.ExtractionRuns().Create(ctx, run); err != nil {
			t.Fatal(err)
		}
	}
	var through, cost int
	if err := db.sql.QueryRowContext(ctx, `SELECT MAX(through_sequence_num) FROM extraction_runs
		WHERE session_id = 's1' AND error = ''`).Scan(&through); err != nil {
		t.Fatal(err)
	}
	if through != 6 {
		t.Errorf("next pass starts after %d, want 6: a failed pass must not move the read point", through)
	}
	if err := db.sql.QueryRowContext(ctx, `SELECT SUM(input_tokens + output_tokens) FROM extraction_runs
		WHERE session_id = 's1'`).Scan(&cost); err != nil {
		t.Fatal(err)
	}
	if cost != 3*920 {
		t.Errorf("extraction cost = %d, want %d: failed passes still count", cost, 3*920)
	}
	var prompt string
	if err := db.sql.QueryRowContext(ctx, `SELECT r.prompt_version FROM extraction_run_notes l
		JOIN extraction_runs r ON r.id = l.run_id WHERE l.note_id = 'n2'`).Scan(&prompt); err != nil || prompt != "v1" {
		t.Errorf("prompt version of note n2 = %q, %v", prompt, err)
	}
}

func TestExtractionRunIsAllOrNothing(t *testing.T) {
	db, ctx := runsFixture(t)
	runs := db.ExtractionRuns()
	if err := runs.Create(ctx, newRun(3, time.Minute, "n1")); err != nil {
		t.Fatal(err)
	}
	rejected := map[string]*session.ExtractionRun{
		"unknown note":          newRun(5, 2*time.Minute, "n2", "no-such-note"),
		"note already in a run": newRun(5, 2*time.Minute, "n2", "n1"),
		"unknown session":       {SessionID: "nope", Provider: "p", Model: "m", PromptVersion: "v1"},
	}
	for name, run := range rejected {
		if err := runs.Create(ctx, run); err == nil {
			t.Errorf("%s: run was accepted", name)
		}
	}
	got, err := runs.ListBySession(ctx, "s1")
	if err != nil || len(got) != 1 {
		t.Fatalf("after rejected runs: %d runs, %v; want the original 1", len(got), err)
	}
	// n2 must not have been left linked by a run that was rolled back.
	if err := runs.Create(ctx, newRun(5, 3*time.Minute, "n2")); err != nil {
		t.Errorf("n2 could not be linked after a rolled-back run: %v", err)
	}

	invalid := map[string]*session.ExtractionRun{
		"nil":               nil,
		"no prompt version": {SessionID: "s1", Provider: "p", Model: "m"},
		"no model":          {SessionID: "s1", Provider: "p", PromptVersion: "v1"},
		"negative tokens":   {SessionID: "s1", Provider: "p", Model: "m", PromptVersion: "v1", InputTokens: -1},
		"malformed note ID": {SessionID: "s1", Provider: "p", Model: "m", PromptVersion: "v1", NoteIDs: []string{"bad id"}},
	}
	for name, run := range invalid {
		if err := runs.Create(ctx, run); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", name, err)
		}
	}
}

func TestDeletingASessionRemovesItsRuns(t *testing.T) {
	db, ctx := runsFixture(t)
	if err := db.ExtractionRuns().Create(ctx, newRun(3, time.Minute, "n1", "n2")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(ctx, `DELETE FROM sessions WHERE id = 's1'`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"extraction_runs", "extraction_run_notes"} {
		var n int
		if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s holds %d rows after its session was deleted (err %v)", table, n, err)
		}
	}
}
