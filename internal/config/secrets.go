package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"
	"unicode"

	"filippo.io/age"
	"filippo.io/age/armor"
)

// Callers match these with errors.Is.
var (
	ErrNoSecret        = errors.New("config: no API key stored")
	ErrWrongPassphrase = errors.New("config: wrong passphrase for the secrets file")
	ErrCorruptSecrets  = errors.New("config: the secrets file is damaged")
)

const (
	maxAPIKeyBytes = 8192

	// generatedWorkFactor is the scrypt cost used when the passphrase is a
	// random 256-bit key from the keyring. Stretching exists to slow down
	// guessing, and such a key cannot be guessed, so the lowest practical
	// cost is used and each read takes milliseconds. A typed passphrase
	// keeps age's default, which takes about a second.
	generatedWorkFactor = 10
)

// AgeSecretStore is the SecretStore backed by an age-encrypted file. Keys are
// decrypted for the length of one call and never held on the struct or
// written to a log (SRS NFR-3, NFR-4).
//
// Beside the encrypted file sits an index: a plain list of the provider IDs
// that have a key. It lets HasAPIKey answer without decrypting anything. It
// holds no secret, and GetAPIKey never trusts it.
type AgeSecretStore struct {
	path string
	pass PassphraseSource
	mu   sync.Mutex
}

var _ SecretStore = (*AgeSecretStore)(nil)

// NewAgeSecretStore returns a store for the secrets file at path. Nothing is
// read or created until the first call.
func NewAgeSecretStore(path string, pass PassphraseSource) *AgeSecretStore {
	return &AgeSecretStore{path: path, pass: pass}
}

func (s *AgeSecretStore) indexPath() string { return s.path + ".index" }

func (s *AgeSecretStore) GetAPIKey(providerID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys, err := s.load()
	if err != nil {
		return "", err
	}
	key, ok := keys[providerID]
	if !ok {
		return "", fmt.Errorf("%w for %q", ErrNoSecret, providerID)
	}
	return key, nil
}

func (s *AgeSecretStore) SetAPIKey(providerID, apiKey string) error {
	if err := checkProviderID(providerID); err != nil {
		return err
	}
	if err := checkAPIKey(apiKey); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	keys, err := s.load()
	if err != nil {
		return err
	}
	keys[providerID] = apiKey
	// Keys first, index second: a crash in between leaves the index saying
	// "no key" for one that exists, which only costs a re-entry prompt.
	if err := s.save(keys); err != nil {
		return err
	}
	return s.saveIndex(keys)
}

// DeleteAPIKey removes a provider's key (SRS FR-1.4). Removing a key that was
// never stored is not an error.
func (s *AgeSecretStore) DeleteAPIKey(providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := keys[providerID]; !ok {
		return nil
	}
	delete(keys, providerID)
	// Index first, for the same reason in reverse.
	if err := s.saveIndex(keys); err != nil {
		return err
	}
	return s.save(keys)
}

// HasAPIKey reports whether a key is stored for the provider. It reads only
// the index, so it needs no passphrase and puts no key in memory.
func (s *AgeSecretStore) HasAPIKey(providerID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids, err := s.loadIndex()
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == providerID {
			return true, nil
		}
	}
	return false, nil
}

// loadIndex reads the index, rebuilding it from the encrypted file if it is
// missing or unreadable.
func (s *AgeSecretStore) loadIndex() ([]string, error) {
	raw, err := os.ReadFile(s.indexPath())
	if err == nil {
		var ids []string
		if json.Unmarshal(raw, &ids) == nil {
			return ids, nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("config: reading the secrets index: %w", err)
	}
	if _, statErr := os.Stat(s.path); errors.Is(statErr, fs.ErrNotExist) {
		return nil, nil
	}
	keys, err := s.load()
	if err != nil {
		return nil, err
	}
	if err := s.saveIndex(keys); err != nil {
		return nil, err
	}
	return sortedIDs(keys), nil
}

func (s *AgeSecretStore) saveIndex(keys map[string]string) error {
	data, err := json.Marshal(sortedIDs(keys))
	if err != nil {
		return fmt.Errorf("config: encoding the secrets index: %w", err)
	}
	if err := writeFileAtomic(s.indexPath(), data); err != nil {
		return fmt.Errorf("config: saving the secrets index: %w", err)
	}
	return nil
}

func sortedIDs(keys map[string]string) []string {
	ids := make([]string, 0, len(keys))
	for id := range keys {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// load decrypts the secrets file. A missing file is an empty set of keys.
func (s *AgeSecretStore) load() (map[string]string, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: reading the secrets file: %w", err)
	}
	pass, err := s.pass.Passphrase()
	if err != nil {
		return nil, err
	}
	identity, err := age.NewScryptIdentity(pass)
	if err != nil {
		return nil, fmt.Errorf("config: passphrase: %w", err)
	}
	reader, err := age.Decrypt(armor.NewReader(bytes.NewReader(raw)), identity)
	if err != nil {
		var noMatch *age.NoIdentityMatchError
		if errors.As(err, &noMatch) {
			return nil, ErrWrongPassphrase
		}
		// The underlying error describes file structure only, never contents.
		return nil, fmt.Errorf("%w: %v", ErrCorruptSecrets, err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCorruptSecrets, err)
	}
	keys := map[string]string{}
	if err := json.Unmarshal(plain, &keys); err != nil {
		// Not wrapped: a JSON error can quote the text it failed on.
		return nil, ErrCorruptSecrets
	}
	return keys, nil
}

func (s *AgeSecretStore) save(keys map[string]string) error {
	pass, err := s.pass.Passphrase()
	if err != nil {
		return err
	}
	recipient, err := age.NewScryptRecipient(pass)
	if err != nil {
		return fmt.Errorf("config: passphrase: %w", err)
	}
	if s.pass.Generated() {
		recipient.SetWorkFactor(generatedWorkFactor)
	}
	plain, err := json.Marshal(keys)
	if err != nil {
		return errors.New("config: encoding secrets failed")
	}
	var buf bytes.Buffer
	armored := armor.NewWriter(&buf)
	encrypted, err := age.Encrypt(armored, recipient)
	if err != nil {
		return fmt.Errorf("config: encrypting secrets: %w", err)
	}
	if _, err := encrypted.Write(plain); err != nil {
		return fmt.Errorf("config: encrypting secrets: %w", err)
	}
	if err := encrypted.Close(); err != nil {
		return fmt.Errorf("config: encrypting secrets: %w", err)
	}
	if err := armored.Close(); err != nil {
		return fmt.Errorf("config: encrypting secrets: %w", err)
	}
	if err := writeFileAtomic(s.path, buf.Bytes()); err != nil {
		return fmt.Errorf("config: saving the secrets file: %w", err)
	}
	return nil
}

// checkAPIKey rejects values that cannot be a real key. The key itself is
// never placed in the error.
func checkAPIKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: API key is empty", ErrInvalid)
	}
	if len(key) > maxAPIKeyBytes {
		return fmt.Errorf("%w: API key is longer than %d bytes", ErrInvalid, maxAPIKeyBytes)
	}
	if strings.IndexFunc(key, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return fmt.Errorf("%w: API key must not contain spaces, line breaks or control characters", ErrInvalid)
	}
	return nil
}
