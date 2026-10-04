package storage

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Callers match these with errors.Is to tell a missing row or bad input from
// a database failure.
var (
	ErrNotFound = errors.New("storage: not found")
	ErrInvalid  = errors.New("storage: invalid input")
)

// Input limits. They stop one bad caller from filling the disk or writing
// control characters that would later be printed to a terminal.
const (
	maxIDBytes      = 128
	maxNameBytes    = 256
	maxTitleBytes   = 512
	maxContentBytes = 4 << 20
	maxTags         = 64
	maxTagBytes     = 64
)

// newID returns a time-ordered UUID, the same kind the notes package assigns.
func newID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("storage: generating id: %w", err)
	}
	return id.String(), nil
}

// ensureID fills an empty ID and validates one the caller supplied.
func ensureID(field string, id *string) error {
	if *id == "" {
		generated, err := newID()
		if err != nil {
			return err
		}
		*id = generated
		return nil
	}
	return checkID(field, *id)
}

// checkID accepts 1 to 128 bytes of [A-Za-z0-9._:-], which keeps IDs safe in
// logs, file names and export files.
func checkID(field, s string) error {
	if s == "" || len(s) > maxIDBytes {
		return fmt.Errorf("%w: %s must be 1-%d characters", ErrInvalid, field, maxIDBytes)
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '.', c == '_', c == ':', c == '-':
		default:
			return fmt.Errorf("%w: %s may only contain letters, digits and . _ : -", ErrInvalid, field)
		}
	}
	return nil
}

// checkName validates a single-line label such as a provider, model or title.
func checkName(field, s string, max int) error {
	if len(s) > max {
		return fmt.Errorf("%w: %s is %d bytes, the limit is %d", ErrInvalid, field, len(s), max)
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("%w: %s is not valid UTF-8", ErrInvalid, field)
	}
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: %s must not contain control characters", ErrInvalid, field)
	}
	return nil
}

// checkContent validates a message or note body. Bodies are stored exactly as
// given, so only size and encoding are checked.
func checkContent(field, s string) error {
	if len(s) > maxContentBytes {
		return fmt.Errorf("%w: %s is %d bytes, the limit is %d", ErrInvalid, field, len(s), maxContentBytes)
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("%w: %s is not valid UTF-8", ErrInvalid, field)
	}
	return nil
}

func checkCount(field string, n int) error {
	if n < 0 {
		return fmt.Errorf("%w: %s must not be negative", ErrInvalid, field)
	}
	return nil
}

// checkProviderModel validates the attribution pair carried by sessions,
// messages, notes and switch events.
func checkProviderModel(prefix, provider, model string) error {
	if err := checkName(prefix+"provider", provider, maxNameBytes); err != nil {
		return err
	}
	return checkName(prefix+"model", model, maxNameBytes)
}
