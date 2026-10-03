package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// Fake keys only: a real credential here would reach CI logs on failure.
const fakeKey = "sk-test-0123456789abcdef"

// countingPass is a typed passphrase that records how often it was asked for.
type countingPass struct {
	value string
	calls int
}

func (p *countingPass) Passphrase() (string, error) { p.calls++; return p.value, nil }
func (p *countingPass) Generated() bool             { return true } // keeps the tests fast

func tempStore(t *testing.T, pass PassphraseSource) (*AgeSecretStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "llmctl", "secrets.age")
	return NewAgeSecretStore(path, pass), path
}

func TestKeyRoundTripAndEncryptionAtRest(t *testing.T) {
	pass := &countingPass{value: "correct horse"}
	s, path := tempStore(t, pass)

	if _, err := s.GetAPIKey("anthropic"); !errors.Is(err, ErrNoSecret) {
		t.Fatalf("Get before Set: got %v, want ErrNoSecret", err)
	}
	if err := s.SetAPIKey("anthropic", fakeKey); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.SetAPIKey("openrouter", fakeKey+"-or"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// A fresh store on the same file sees both keys.
	again := NewAgeSecretStore(path, &countingPass{value: "correct horse"})
	if got, err := again.GetAPIKey("anthropic"); err != nil || got != fakeKey {
		t.Errorf("Get anthropic = %q, %v", got, err)
	}
	if got, err := again.GetAPIKey("openrouter"); err != nil || got != fakeKey+"-or" {
		t.Errorf("Get openrouter = %q, %v", got, err)
	}
	// Ollama has no key; that is a typed error, not a failure.
	if _, err := again.GetAPIKey("ollama"); !errors.Is(err, ErrNoSecret) {
		t.Errorf("Get ollama: got %v, want ErrNoSecret", err)
	}

	for _, file := range []string{path, path + ".index"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		if strings.Contains(string(data), fakeKey) || strings.Contains(string(data), "sk-test") {
			t.Errorf("%s contains the key in plaintext", filepath.Base(file))
		}
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), "-----BEGIN AGE ENCRYPTED FILE-----") {
		t.Errorf("secrets file is not an armored age file: %.40q", data)
	}
}

func TestHasAPIKeyNeedsNoPassphrase(t *testing.T) {
	s, path := tempStore(t, &countingPass{value: "pw"})
	if has, err := s.HasAPIKey("anthropic"); err != nil || has {
		t.Fatalf("Has on a fresh store = %v, %v; want false", has, err)
	}
	if err := s.SetAPIKey("anthropic", fakeKey); err != nil {
		t.Fatal(err)
	}

	pass := &countingPass{value: "pw"}
	again := NewAgeSecretStore(path, pass)
	if has, err := again.HasAPIKey("anthropic"); err != nil || !has {
		t.Errorf("Has anthropic = %v, %v; want true", has, err)
	}
	if has, err := again.HasAPIKey("ollama"); err != nil || has {
		t.Errorf("Has ollama = %v, %v; want false", has, err)
	}
	if pass.calls != 0 {
		t.Errorf("HasAPIKey asked for the passphrase %d times, want 0", pass.calls)
	}

	if err := again.DeleteAPIKey("anthropic"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if has, _ := again.HasAPIKey("anthropic"); has {
		t.Error("Has is still true after Delete")
	}
	if _, err := again.GetAPIKey("anthropic"); !errors.Is(err, ErrNoSecret) {
		t.Errorf("Get after Delete: got %v, want ErrNoSecret", err)
	}
	if err := again.DeleteAPIKey("anthropic"); err != nil {
		t.Errorf("deleting an absent key: %v", err)
	}
}

func TestLostIndexIsRebuilt(t *testing.T) {
	s, path := tempStore(t, &countingPass{value: "pw"})
	if err := s.SetAPIKey("anthropic", fakeKey); err != nil {
		t.Fatal(err)
	}
	for name, damage := range map[string]func() error{
		"deleted": func() error { return os.Remove(path + ".index") },
		"garbled": func() error { return os.WriteFile(path+".index", []byte("{not json"), 0o600) },
	} {
		if err := damage(); err != nil {
			t.Fatal(err)
		}
		if has, err := s.HasAPIKey("anthropic"); err != nil || !has {
			t.Errorf("index %s: Has = %v, %v; want true", name, has, err)
		}
		if _, err := os.Stat(path + ".index"); err != nil {
			t.Errorf("index %s: not rewritten: %v", name, err)
		}
	}
}

func TestWrongPassphraseAndDamagedFile(t *testing.T) {
	s, path := tempStore(t, &countingPass{value: "right"})
	if err := s.SetAPIKey("anthropic", fakeKey); err != nil {
		t.Fatal(err)
	}

	wrong := NewAgeSecretStore(path, &countingPass{value: "wrong"})
	if _, err := wrong.GetAPIKey("anthropic"); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("wrong passphrase: got %v, want ErrWrongPassphrase", err)
	}
	// A wrong passphrase must not be able to overwrite the existing keys.
	if err := wrong.SetAPIKey("openrouter", fakeKey); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("Set with wrong passphrase: got %v, want ErrWrongPassphrase", err)
	}
	if got, err := s.GetAPIKey("anthropic"); err != nil || got != fakeKey {
		t.Errorf("original key after failed Set = %q, %v", got, err)
	}

	if err := os.WriteFile(path, []byte("this is not an age file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAPIKey("anthropic"); !errors.Is(err, ErrCorruptSecrets) {
		t.Errorf("damaged file: got %v, want ErrCorruptSecrets", err)
	}
}

func TestRejectsBadKeysWithoutEchoingThem(t *testing.T) {
	s, _ := tempStore(t, &countingPass{value: "pw"})
	bad := map[string][2]string{
		"empty key":        {"anthropic", ""},
		"key with space":   {"anthropic", "sk-secret value"},
		"key with newline": {"anthropic", "sk-secret\nvalue"},
		"overlong key":     {"anthropic", strings.Repeat("k", maxAPIKeyBytes+1)},
		"bad provider ID":  {"Anthropic!", fakeKey},
	}
	for name, c := range bad {
		err := s.SetAPIKey(c[0], c[1])
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: got %v, want ErrInvalid", name, err)
		}
		if err != nil && strings.Contains(err.Error(), "sk-secret") {
			t.Errorf("%s: error echoes the key: %v", name, err)
		}
	}
}

func TestKeyringPassphraseIsCreatedOnceAndReused(t *testing.T) {
	keyring.MockInit()
	t.Setenv("LLMCTL_NO_KEYRING", "")

	source := KeyringPassphrase{}
	first, err := source.Passphrase()
	if err != nil {
		t.Fatalf("first Passphrase: %v", err)
	}
	if len(first) < 40 {
		t.Errorf("generated passphrase is only %d characters", len(first))
	}
	second, err := source.Passphrase()
	if err != nil || second != first {
		t.Errorf("second Passphrase differs from the first (err %v)", err)
	}
	stored, err := keyring.Get(keyringService, keyringUser)
	if err != nil || stored != first {
		t.Errorf("keyring entry does not hold the passphrase (err %v)", err)
	}
	if !source.Generated() {
		t.Error("a keyring passphrase should report Generated")
	}

	// End to end: no prompt, and a second store unlocks the first one's file.
	s, path := tempStore(t, source)
	if err := s.SetAPIKey("anthropic", fakeKey); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, err := NewAgeSecretStore(path, KeyringPassphrase{}).GetAPIKey("anthropic"); err != nil || got != fakeKey {
		t.Errorf("Get through the keyring = %q, %v", got, err)
	}
}

func TestKeyringFailureDoesNotReplaceThePassphrase(t *testing.T) {
	keyring.MockInitWithError(errors.New("keyring service not running"))
	t.Setenv("LLMCTL_NO_KEYRING", "")
	if _, err := (KeyringPassphrase{}).Passphrase(); err == nil {
		t.Error("an unreachable keyring returned a passphrase")
	}
}

func TestNoKeyringFallsBackToATypedPassphrase(t *testing.T) {
	keyring.MockInit()
	t.Setenv("LLMCTL_NO_KEYRING", "1")

	typed := NewTypedPassphrase(func() (string, error) { return "typed by hand", nil })
	source := KeyringPassphrase{Fallback: typed}
	got, err := source.Passphrase()
	if err != nil || got != "typed by hand" {
		t.Errorf("Passphrase = %q, %v; want the typed one", got, err)
	}
	if source.Generated() {
		t.Error("a typed passphrase must not report Generated")
	}
	if _, err := keyring.Get(keyringService, keyringUser); !errors.Is(err, keyring.ErrNotFound) {
		t.Errorf("the keyring was written to although it is disabled (err %v)", err)
	}
	if _, err := (KeyringPassphrase{}).Passphrase(); err == nil {
		t.Error("no keyring and no fallback returned a passphrase")
	}
	empty := NewTypedPassphrase(func() (string, error) { return "", nil })
	if _, err := empty.Passphrase(); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty typed passphrase: got %v, want ErrInvalid", err)
	}

	// The slow, typed path still round-trips.
	s, _ := tempStore(t, source)
	if err := s.SetAPIKey("anthropic", fakeKey); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, err := s.GetAPIKey("anthropic"); err != nil || got != fakeKey {
		t.Errorf("Get = %q, %v", got, err)
	}
}

func TestTypedPassphraseIsAskedForOncePerProcess(t *testing.T) {
	keyring.MockInit()
	t.Setenv("LLMCTL_NO_KEYRING", "1")

	prompts, fail := 0, true
	typed := NewTypedPassphrase(func() (string, error) {
		prompts++
		if fail {
			return "", errors.New("prompt cancelled")
		}
		return "typed by hand", nil
	})
	s, _ := tempStore(t, KeyringPassphrase{Fallback: typed})

	// A cancelled prompt is not remembered: the next call asks again.
	if err := s.SetAPIKey("anthropic", fakeKey); err == nil {
		t.Fatal("Set succeeded although the prompt was cancelled")
	}
	fail = false
	if err := s.SetAPIKey("anthropic", fakeKey); err != nil {
		t.Fatalf("Set: %v", err)
	}
	asked := prompts
	for i := 0; i < 3; i++ {
		if _, err := s.GetAPIKey("anthropic"); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}
	if prompts != asked {
		t.Errorf("the user was prompted %d more times after the first answer", prompts-asked)
	}
	if _, err := (*TypedPassphrase)(NewTypedPassphrase(nil)).Passphrase(); err == nil {
		t.Error("a missing prompt returned a passphrase")
	}
}

// TestNoPlaintextKeyAnywhereOnDisk walks everything the stores wrote and
// checks that the key appears in none of it (SRS FR-1.2).
func TestNoPlaintextKeyAnywhereOnDisk(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadTOML(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetProviderConfig(ProviderConfig{ID: "anthropic", DisplayName: "Anthropic",
		BaseURL: "https://api.anthropic.com", DefaultModel: "claude-haiku-4-5", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	secrets := NewAgeSecretStore(filepath.Join(dir, "secrets.age"), &countingPass{value: "pw"})
	if err := secrets.SetAPIKey("anthropic", fakeKey); err != nil {
		t.Fatal(err)
	}
	if _, err := secrets.GetAPIKey("anthropic"); err != nil {
		t.Fatal(err)
	}

	files := 0
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), fakeKey) {
			t.Errorf("%s contains the API key in plaintext", filepath.Base(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files != 3 {
		t.Errorf("found %d files, want config.toml, secrets.age and secrets.age.index", files)
	}
}
