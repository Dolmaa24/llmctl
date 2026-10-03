package storage

import (
	"context"
	"fmt"
	"time"
)

// ProviderRow is one row of the providers table: non-secret provider
// metadata, never a key.
//
// config.toml is the single source of truth for provider configuration. This
// table is a write-through mirror of it, kept so the schema in the Technical
// Architecture document holds and provider metadata can be joined against
// session history. Nothing reads it back as authoritative. The type repeats
// config.ProviderConfig's fields so that storage does not import config.
type ProviderRow struct {
	ID           string
	DisplayName  string
	BaseURL      string
	DefaultModel string
	Enabled      bool
}

// UpsertProvider writes a provider's row. The integrator calls it from
// config.TOMLStore's OnChange hook whenever a provider is saved.
func (db *DB) UpsertProvider(ctx context.Context, p ProviderRow) error {
	if err := checkID("provider id", p.ID); err != nil {
		return err
	}
	now := encodeTime(time.Now())
	_, err := db.sql.ExecContext(ctx,
		`INSERT INTO providers (id, display_name, base_url, default_model, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   display_name = excluded.display_name,
		   base_url = excluded.base_url,
		   default_model = excluded.default_model,
		   enabled = excluded.enabled,
		   updated_at = excluded.updated_at`,
		p.ID, p.DisplayName, p.BaseURL, p.DefaultModel, p.Enabled, now, now)
	if err != nil {
		return fmt.Errorf("storage: mirroring provider %s: %w", p.ID, err)
	}
	return nil
}

// DeleteProvider removes a provider's row. Removing one that is not there is
// not an error: the mirror only has to end up matching config.toml.
func (db *DB) DeleteProvider(ctx context.Context, id string) error {
	if _, err := db.sql.ExecContext(ctx, `DELETE FROM providers WHERE id = ?`, id); err != nil {
		return fmt.Errorf("storage: removing provider %s: %w", id, err)
	}
	return nil
}

// Providers returns the mirrored rows sorted by ID.
func (db *DB) Providers(ctx context.Context) ([]ProviderRow, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT id, display_name, base_url, default_model, enabled FROM providers ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("storage: listing providers: %w", err)
	}
	defer rows.Close()
	var out []ProviderRow
	for rows.Next() {
		var p ProviderRow
		if err := rows.Scan(&p.ID, &p.DisplayName, &p.BaseURL, &p.DefaultModel, &p.Enabled); err != nil {
			return nil, fmt.Errorf("storage: listing providers: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
