package keyring

import (
	"context"
	"sync"
)

// Memory is an in-process Keyring implementation. It is used in tests and in
// CI where no OS credential store is available. It is NOT a secure store and
// must never be selected on a real user's machine.
type Memory struct {
	mu     sync.Mutex
	secret string
	set    bool
}

// NewMemory returns an empty in-memory keyring.
func NewMemory() *Memory { return &Memory{} }

func (m *Memory) Set(_ context.Context, secret string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.secret = secret
	m.set = true
	return nil
}

func (m *Memory) Get(_ context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.set {
		return "", ErrNotFound
	}
	return m.secret, nil
}

func (m *Memory) Delete(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.set {
		return ErrNotFound
	}
	m.secret = ""
	m.set = false
	return nil
}

func (m *Memory) Backend() string { return "in-memory (insecure)" }
