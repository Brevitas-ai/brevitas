// Package keyring stores the single Brevitas API key in the operating
// system's native credential store:
//
//	macOS   -> Keychain (via the `security` tool)
//	Windows -> Credential Manager (via advapi32 Cred* APIs)
//	Linux   -> Secret Service (via `secret-tool`, libsecret)
//
// The key is never written to disk in plaintext. When no secure store is
// available, operations return ErrUnavailable rather than silently degrading.
package keyring

import (
	"context"
	"errors"
	"strings"
)

// Service and account identifiers used for the Brevitas API key entry.
const (
	Service = "com.brevitas.cli"
	Account = "brevitas-api-key"
)

// Sentinel errors.
var (
	// ErrNotFound indicates no key is stored.
	ErrNotFound = errors.New("keyring: item not found")
	// ErrUnavailable indicates no secure credential store is usable on this
	// host (e.g. no Secret Service daemon on a headless Linux box).
	ErrUnavailable = errors.New("keyring: no secure credential store available")
)

// Keyring abstracts a secret store, enabling injection of a fake in tests.
type Keyring interface {
	Set(ctx context.Context, secret string) error
	Get(ctx context.Context) (string, error)
	Delete(ctx context.Context) error
	Backend() string
}

// Default returns the platform's native keyring implementation.
func Default() Keyring { return osKeyring{} }

// osKeyring dispatches to platform-specific functions implemented in the
// build-tagged files (keyring_darwin.go, keyring_linux.go, keyring_windows.go).
type osKeyring struct{}

func (osKeyring) Set(ctx context.Context, secret string) error {
	return osSet(ctx, strings.TrimSpace(secret))
}
func (osKeyring) Get(ctx context.Context) (string, error) { return osGet(ctx) }
func (osKeyring) Delete(ctx context.Context) error        { return osDelete(ctx) }
func (osKeyring) Backend() string                         { return osBackend() }
