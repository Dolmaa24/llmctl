package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver: no C toolchain on any machine
)

// pragmas are applied to every connection the driver opens. Foreign keys are
// off by default in SQLite and are per connection, so setting them once with
// Exec would silently stop applying if the pool ever reconnected. WAL keeps a
// crash from losing more than the write in flight (SRS NFR-6).
const pragmas = "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"

// DB is an open llmctl database. It hands out the repositories; nothing
// outside this package sees the SQL handle.
type DB struct {
	sql *sql.DB
}

// Open opens the database file at path, creating it and its directory if
// needed, and brings the schema up to date.
func Open(ctx context.Context, path string) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: database path is empty", ErrInvalid)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("storage: creating database directory: %w", err)
	}
	handle, err := sql.Open("sqlite", path+pragmas)
	if err != nil {
		return nil, fmt.Errorf("storage: opening %s: %w", path, err)
	}
	// SQLite allows one writer at a time. A single connection makes that
	// explicit: writes queue in Go instead of failing with "database is locked".
	handle.SetMaxOpenConns(1)
	if err := handle.PingContext(ctx); err != nil {
		handle.Close()
		return nil, fmt.Errorf("storage: opening %s: %w", path, err)
	}
	if err := Migrate(handle); err != nil {
		handle.Close()
		return nil, err
	}
	return &DB{sql: handle}, nil
}

// Close releases the database file.
func (db *DB) Close() error { return db.sql.Close() }

func (db *DB) Sessions() SessionsRepo             { return &sessionsRepo{db.sql} }
func (db *DB) Messages() MessagesRepo             { return &messagesRepo{db.sql} }
func (db *DB) Notes() NotesRepo                   { return &notesRepo{db.sql} }
func (db *DB) SwitchEvents() SwitchEventsRepo     { return &switchEventsRepo{db.sql} }
func (db *DB) ExtractionRuns() ExtractionRunsRepo { return &extractionRunsRepo{db.sql} }

// timeLayout is fixed-width and always UTC, so timestamps sort correctly as
// text. Queries that compare created_at across tables depend on that.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

func encodeTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// orNow returns t, or the current time when the caller left it unset.
func orNow(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t.UTC().Round(0)
}

// dbTime reads a timestamp column. The driver returns a time.Time when it
// recognises the text and a string when it does not, so both are accepted.
type dbTime struct{ time.Time }

func (d *dbTime) Scan(src any) error {
	switch v := src.(type) {
	case time.Time:
		d.Time = v.UTC()
		return nil
	case string:
		return d.parse(v)
	case []byte:
		return d.parse(string(v))
	}
	return fmt.Errorf("storage: cannot read %T as a timestamp", src)
}

func (d *dbTime) parse(s string) error {
	// The second layout is what SQLite's own CURRENT_TIMESTAMP writes.
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			d.Time = t.UTC()
			return nil
		}
	}
	return fmt.Errorf("storage: unrecognised timestamp %q", s)
}
