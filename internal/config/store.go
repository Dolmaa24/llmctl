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
}
