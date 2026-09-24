package storage

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
)

// migrationFS carries the schema inside the binary, so a user cannot lose or
// edit the migration files.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate applies every embedded migration in filename order. Migrations are
// written to be idempotent, so re-running them on an existing database is safe.
func Migrate(db *sql.DB) error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("storage: reading migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		stmt, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("storage: reading %s: %w", name, err)
		}
		if _, err := db.Exec(string(stmt)); err != nil {
			return fmt.Errorf("storage: applying %s: %w", name, err)
		}
	}
	return nil
}
