// Package config loads non-secret configuration from TOML and decrypts API
// keys on demand from an age-encrypted store whose passphrase lives in the OS
// keyring.
package config

// ProviderConfig is non-secret provider metadata. API keys never appear here.
type ProviderConfig struct {
	ID           string // "anthropic", "openrouter", "ollama"
	DisplayName  string
	BaseURL      string
	DefaultModel string
	Enabled      bool
}

// Store reads and writes non-secret configuration.
type Store interface {
	GetProviderConfig(providerID string) (ProviderConfig, error)
	SetProviderConfig(cfg ProviderConfig) error
}

// SecretStore holds API keys encrypted at rest.
//
// Decrypted keys must exist in memory only for the duration of a call and must
// never be logged, at any level including debug (SRS NFR-4).
type SecretStore interface {
	GetAPIKey(providerID string) (string, error)
	SetAPIKey(providerID, apiKey string) error

	// HasAPIKey reports whether a key is stored for the provider, without
	// decrypting it. Callers that only need to know a key exists (the
	// provider form, the provider list) must use this rather than GetAPIKey,
	// so no key is brought into memory just to be discarded.
	HasAPIKey(providerID string) (bool, error)
}
