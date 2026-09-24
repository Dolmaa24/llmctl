package storage

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// openTemp opens a fresh database in the test's temp directory. The pure-Go
// driver is deliberate: a CGO-linked driver would break cross-compilation to
// Windows from a macOS machine, which half this team depends on.
func openTemp(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "llmctl.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enabling foreign keys: %v", err)
	}
	return db
}

func TestMigrateCreatesEverySchemaObject(t *testing.T) {
	db := openTemp(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, table := range []string{"sessions", "messages", "notes", "switch_events", "providers"} {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q missing after migration: %v", table, err)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := openTemp(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate should be a no-op, got: %v", err)
	}
}

func TestForeignKeysAreEnforced(t *testing.T) {
	db := openTemp(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// A message referencing a session that does not exist must be rejected.
	_, err := db.Exec(
		`INSERT INTO messages (id, session_id, sequence_num, role, content, created_at)
		 VALUES ('m1', 'no-such-session', 0, 'user', 'hello', CURRENT_TIMESTAMP)`,
	)
	if err == nil {
		t.Fatal("expected foreign key violation, got nil error")
	}
}

func TestSequenceNumberIsUniquePerSession(t *testing.T) {
	db := openTemp(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	mustExec(t, db,
		`INSERT INTO sessions (id, created_at, updated_at)
		 VALUES ('s1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	mustExec(t, db,
		`INSERT INTO messages (id, session_id, sequence_num, role, content, created_at)
		 VALUES ('m1', 's1', 0, 'user', 'hello', CURRENT_TIMESTAMP)`)

	_, err := db.Exec(
		`INSERT INTO messages (id, session_id, sequence_num, role, content, created_at)
		 VALUES ('m2', 's1', 0, 'assistant', 'hi', CURRENT_TIMESTAMP)`)
	if err == nil {
		t.Fatal("expected duplicate sequence_num to be rejected, got nil error")
	}
}

func TestRoleIsConstrained(t *testing.T) {
	db := openTemp(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	mustExec(t, db,
		`INSERT INTO sessions (id, created_at, updated_at)
		 VALUES ('s1', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)

	_, err := db.Exec(
		`INSERT INTO messages (id, session_id, sequence_num, role, content, created_at)
		 VALUES ('m1', 's1', 0, 'robot', 'hello', CURRENT_TIMESTAMP)`)
	if err == nil {
		t.Fatal("expected invalid role to be rejected, got nil error")
	}
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}
