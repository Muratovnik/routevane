//go:build routevane_testmode

package secretstore

import (
	"context"
	"errors"
	"sync"
)

// Memory keeps credentials in this process and nowhere else. It exists only in
// a test build (ADR 0052): a suite that saves a connection password must never
// write to the workstation's own credential store, and a release build must
// never keep a password in memory as if it were saved. Everything it holds is
// gone when the process exits.
//
// It answers exactly as Store does, including ErrNotFound for an absent key,
// and never puts a key's secret into an error.
type Memory struct {
	mu      sync.Mutex
	secrets map[string]string
}

// NewMemory returns an empty in-process store.
func NewMemory() *Memory { return &Memory{secrets: map[string]string{}} }

// Available reports a store, so the surface offers to save a password exactly
// as it does on a platform with one.
func (*Memory) Available() bool { return true }

func (m *Memory) PutSecret(_ context.Context, key, _, secret string) error {
	if key == "" || secret == "" {
		return errors.New("invalid credential")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.secrets[key] = secret
	return nil
}

func (m *Memory) Secret(_ context.Context, key string) (string, error) {
	if key == "" {
		return "", errors.New("invalid credential key")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	secret, ok := m.secrets[key]
	if !ok {
		return "", ErrNotFound
	}
	return secret, nil
}

// DeleteSecret removes the entry; an absent entry is success, as in Store.
func (m *Memory) DeleteSecret(_ context.Context, key string) error {
	if key == "" {
		return errors.New("invalid credential key")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.secrets, key)
	return nil
}
