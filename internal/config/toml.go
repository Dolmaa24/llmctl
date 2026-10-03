package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/BurntSushi/toml"
)

const (
	maxProviderIDBytes = 64
	maxFieldBytes      = 256
	maxURLBytes        = 2048
)

// tomlFile is the on-disk shape of config.toml. It never holds an API key.
//
// There is deliberately no global extraction provider or model here:
// extraction is chosen per conversation, so a local chat is never sent to a
// hosted provider because of a setting made elsewhere.
type tomlFile struct {
	Providers map[string]tomlProvider `toml:"providers"`
}

type tomlProvider struct {
	DisplayName  string `toml:"display_name"`
	BaseURL      string `toml:"base_url,omitempty"`
	DefaultModel string `toml:"default_model"`
	Enabled      bool   `toml:"enabled"`
}

// TOMLStore is the Store backed by config.toml. It is safe for concurrent use.
type TOMLStore struct {
	path string

	mu        sync.Mutex
	providers map[string]ProviderConfig
}

var _ Store = (*TOMLStore)(nil)

// LoadTOML reads the config file at path. A missing file is an empty
// configuration, not an error: that is the state on first run.
func LoadTOML(path string) (*TOMLStore, error) {
	s := &TOMLStore{path: path, providers: map[string]ProviderConfig{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}
	var file tomlFile
	if _, err := toml.Decode(string(data), &file); err != nil {
		return nil, fmt.Errorf("config: %s is not valid TOML: %w", path, err)
	}
	for id, p := range file.Providers {
		cfg := ProviderConfig{ID: id, DisplayName: p.DisplayName, BaseURL: p.BaseURL,
			DefaultModel: p.DefaultModel, Enabled: p.Enabled}
		// The file is hand-editable, so it is checked like any other input.
		if err := validate(cfg); err != nil {
			return nil, fmt.Errorf("config: %s: provider %q: %w", path, id, err)
		}
		s.providers[id] = cfg
	}
	return s, nil
}

func (s *TOMLStore) GetProviderConfig(providerID string) (ProviderConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, ok := s.providers[providerID]
	if !ok {
		return ProviderConfig{}, fmt.Errorf("%w: %q", ErrNotConfigured, providerID)
	}
	return cfg, nil
}

// SetProviderConfig adds or replaces a provider and saves the file. If the
// save fails the in-memory configuration is left as it was.
func (s *TOMLStore) SetProviderConfig(cfg ProviderConfig) error {
	if err := validate(cfg); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.providers[cfg.ID]
	s.providers[cfg.ID] = cfg
	if err := s.save(); err != nil {
		if existed {
			s.providers[cfg.ID] = previous
		} else {
			delete(s.providers, cfg.ID)
		}
		return err
	}
	return nil
}

// DeleteProvider removes a provider profile (SRS FR-1.4).
func (s *TOMLStore) DeleteProvider(providerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.providers[providerID]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotConfigured, providerID)
	}
	delete(s.providers, providerID)
	if err := s.save(); err != nil {
		s.providers[providerID] = previous
		return err
	}
	return nil
}

// Providers returns every configured provider, sorted by ID. The provider
// pane and the doctor's endpoint checks both need the full list (FR-1.5).
func (s *TOMLStore) Providers() []ProviderConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ProviderConfig, 0, len(s.providers))
	for _, cfg := range s.providers {
		out = append(out, cfg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *TOMLStore) save() error {
	file := tomlFile{Providers: make(map[string]tomlProvider, len(s.providers))}
	for id, cfg := range s.providers {
		file.Providers[id] = tomlProvider{DisplayName: cfg.DisplayName, BaseURL: cfg.BaseURL,
			DefaultModel: cfg.DefaultModel, Enabled: cfg.Enabled}
	}
	var buf bytes.Buffer
	buf.WriteString("# llmctl configuration. No secrets live here: API keys are in secrets.age.\n")
	if err := toml.NewEncoder(&buf).Encode(file); err != nil {
		return fmt.Errorf("config: encoding: %w", err)
	}
	if err := writeFileAtomic(s.path, buf.Bytes()); err != nil {
		return fmt.Errorf("config: saving %s: %w", s.path, err)
	}
	return nil
}

func validate(cfg ProviderConfig) error {
	if err := checkProviderID(cfg.ID); err != nil {
		return err
	}
	if err := checkField("display name", cfg.DisplayName); err != nil {
		return err
	}
	if err := checkField("default model", cfg.DefaultModel); err != nil {
		return err
	}
	return checkBaseURL(cfg.BaseURL)
}

// checkProviderID keeps IDs to lower-case letters, digits, - and _. The ID
// becomes a TOML table name and part of doctor check names such as
// "auth:anthropic".
func checkProviderID(id string) error {
	if id == "" || len(id) > maxProviderIDBytes {
		return fmt.Errorf("%w: provider ID must be 1-%d characters", ErrInvalid, maxProviderIDBytes)
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return fmt.Errorf("%w: provider ID may only contain lower-case letters, digits, - and _", ErrInvalid)
		}
	}
	return nil
}

func checkField(name, s string) error {
	if len(s) > maxFieldBytes {
		return fmt.Errorf("%w: %s is longer than %d bytes", ErrInvalid, name, maxFieldBytes)
	}
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: %s must not contain control characters", ErrInvalid, name)
	}
	return nil
}

// checkBaseURL enforces where a key may be sent: over HTTPS, or over plain
// HTTP only to this machine, which is how Ollama listens. The URL is never
// echoed in the error because a mistyped one may contain a credential.
func checkBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	if len(raw) > maxURLBytes || strings.IndexFunc(raw, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) >= 0 {
		return fmt.Errorf("%w: base URL must be at most %d characters with no spaces", ErrInvalid, maxURLBytes)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("%w: base URL is not a valid absolute URL", ErrInvalid)
	}
	if u.User != nil {
		return fmt.Errorf("%w: base URL must not contain credentials; save the key separately", ErrInvalid)
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("%w: base URL must not contain a query string or fragment", ErrInvalid)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopback(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("%w: base URL uses plain http to another machine; use https", ErrInvalid)
	}
	return fmt.Errorf("%w: base URL must start with https:// (or http:// for this machine)", ErrInvalid)
}

func isLoopback(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Unmap().IsLoopback()
}
