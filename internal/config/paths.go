package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Callers match these with errors.Is.
var (
	ErrNotConfigured = errors.New("config: provider not configured")
	ErrInvalid       = errors.New("config: invalid input")
)

// Paths are the files llmctl keeps on disk (Technical Architecture section 6).
type Paths struct {
	Dir     string // holds everything below unless overridden
	Config  string // config.toml: non-secret settings
	Secrets string // secrets.age: encrypted API keys
	DB      string // llmctl.db: session history
}

// DefaultPaths resolves where llmctl stores its files.
//
// LLMCTL_CONFIG_DIR and LLMCTL_DB_PATH win when set. Otherwise the directory
// is os.UserConfigDir()/llmctl: %APPDATA%\llmctl on Windows and the Linux home
// directory inside WSL. That keeps the database off /mnt/c, where SQLite file
// locking is unreliable, without any OS branching here (SRS NFR-7).
func DefaultPaths() (Paths, error) {
	dir := os.Getenv("LLMCTL_CONFIG_DIR")
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return Paths{}, fmt.Errorf("config: cannot locate the user config directory (set LLMCTL_CONFIG_DIR): %w", err)
		}
		dir = filepath.Join(base, "llmctl")
	}
	db := os.Getenv("LLMCTL_DB_PATH")
	if db == "" {
		db = filepath.Join(dir, "llmctl.db")
	}
	return Paths{
		Dir:     dir,
		Config:  filepath.Join(dir, "config.toml"),
		Secrets: filepath.Join(dir, "secrets.age"),
		DB:      db,
	}, nil
}

// writeFileAtomic replaces path with data through a temporary file and a
// rename, so a crash mid-write leaves the old file intact rather than a
// truncated one. Access is governed by the directory: file modes such as 0600
// do nothing on Windows, where the user profile's access rules protect it.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
