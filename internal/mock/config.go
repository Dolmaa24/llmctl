package mock

import (
	"fmt"
	"sync"

	"github.com/Dolmaa24/llmctl/internal/config"
)

// ConfigStore is an in-memory config.Store.
type ConfigStore struct {
	mu   sync.Mutex
	rows map[string]config.ProviderConfig
}

func NewConfigStore() *ConfigStore {
	return &ConfigStore{rows: map[string]config.ProviderConfig{}}
}

func (s *ConfigStore) GetProviderConfig(providerID string) (config.ProviderConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.rows[providerID]
	if !ok {
		return config.ProviderConfig{}, fmt.Errorf("mock: no config for %q", providerID)
	}
	return c, nil
}

func (s *ConfigStore) SetProviderConfig(cfg config.ProviderConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rows == nil {
		s.rows = map[string]config.ProviderConfig{}
	}
	s.rows[cfg.ID] = cfg
	return nil
}

// SecretStore is an in-memory config.SecretStore.
//
// It holds fake keys only. No test may place a real credential here — the
// value would reach CI logs on failure.
type SecretStore struct {
	mu   sync.Mutex
	keys map[string]string
}

func NewSecretStore() *SecretStore {
	return &SecretStore{keys: map[string]string{}}
}

func (s *SecretStore) GetAPIKey(providerID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k, ok := s.keys[providerID]
	if !ok {
		return "", fmt.Errorf("mock: no key for %q", providerID)
	}
	return k, nil
}

func (s *SecretStore) SetAPIKey(providerID, apiKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keys == nil {
		s.keys = map[string]string{}
	}
	s.keys[providerID] = apiKey
	return nil
}

func (s *SecretStore) HasAPIKey(providerID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.keys[providerID]
	return ok, nil
}
