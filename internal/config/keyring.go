package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/zalando/go-keyring"
)

// The single keyring entry llmctl owns. On Windows it appears in Credential
// Manager as "llmctl:secrets-passphrase".
const (
	keyringService = "llmctl"
	keyringUser    = "secrets-passphrase"
)

// PassphraseSource supplies the passphrase that unlocks the secrets file.
type PassphraseSource interface {
	Passphrase() (string, error)

	// Generated reports whether the passphrase is a random key made by
	// llmctl rather than something a person typed. A random key cannot be
	// guessed, so the secrets file can skip the slow key-stretching a typed
	// passphrase needs, and reading an API key stays instant.
	Generated() bool
}

// TypedPassphrase is a passphrase a person enters. The user is asked once per
// process: the answer is then kept in memory only, never on disk, so reading
// several keys does not prompt several times. A prompt that fails or returns
// nothing is not remembered, and the next call asks again.
type TypedPassphrase struct {
	prompt func() (string, error)

	mu    sync.Mutex
	value string
}

// NewTypedPassphrase wraps a prompt. cmd/llmctl supplies one that reads from
// the terminal without echo.
func NewTypedPassphrase(prompt func() (string, error)) *TypedPassphrase {
	return &TypedPassphrase{prompt: prompt}
}

func (t *TypedPassphrase) Passphrase() (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.value != "" {
		return t.value, nil
	}
	if t.prompt == nil {
		return "", errors.New("config: no passphrase prompt is available")
	}
	pass, err := t.prompt()
	if err != nil {
		return "", err
	}
	if pass == "" {
		return "", fmt.Errorf("%w: passphrase is empty", ErrInvalid)
	}
	t.value = pass
	return pass, nil
}

func (*TypedPassphrase) Generated() bool { return false }

// KeyringPassphrase keeps the passphrase in the OS keyring: the keyring holds
// the passphrase and the age file holds the keys (Technical Architecture
// section 6). The first use creates a random passphrase and stores it.
//
// When LLMCTL_NO_KEYRING is set, Fallback is used instead. WSL often has no
// keyring service running, so that is the supported path there.
type KeyringPassphrase struct {
	Fallback PassphraseSource
}

// KeyringDisabled reports whether LLMCTL_NO_KEYRING is set.
func KeyringDisabled() bool { return os.Getenv("LLMCTL_NO_KEYRING") != "" }

func (k KeyringPassphrase) Passphrase() (string, error) {
	if KeyringDisabled() {
		if k.Fallback == nil {
			return "", errors.New("config: LLMCTL_NO_KEYRING is set but no passphrase prompt is available")
		}
		return k.Fallback.Passphrase()
	}
	pass, err := keyring.Get(keyringService, keyringUser)
	if err == nil && pass != "" {
		return pass, nil
	}
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		// Do not create a new passphrase here: the old one may still exist
		// behind a keyring that is only temporarily unreachable, and
		// replacing it would lock the user out of every saved key.
		return "", fmt.Errorf("config: the OS keyring is unavailable (set LLMCTL_NO_KEYRING to use a typed passphrase): %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("config: generating passphrase: %w", err)
	}
	pass = base64.RawURLEncoding.EncodeToString(raw)
	if err := keyring.Set(keyringService, keyringUser, pass); err != nil {
		return "", fmt.Errorf("config: saving passphrase to the OS keyring: %w", err)
	}
	return pass, nil
}

func (k KeyringPassphrase) Generated() bool {
	if KeyringDisabled() {
		return k.Fallback != nil && k.Fallback.Generated()
	}
	return true
}
