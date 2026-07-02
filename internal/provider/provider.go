// Package provider defines the Provider abstraction that every supported AI
// tool integration implements, plus dependency-injected helpers for safe
// configuration rewriting (with backups and rollback).
//
// The interface deliberately extends the minimal spec signature by threading a
// context.Context through every call. This keeps cancellation, deadlines, and
// request-scoped logging idiomatic across the codebase while preserving the
// spec's Name/Detect/Install/Uninstall/Validate/Status surface.
package provider

import (
	"context"
	"log/slog"

	"github.com/Brevitas-ai/brevitas/internal/config"
)

// Support classifies how completely Brevitas can integrate with a tool.
type Support string

const (
	// SupportFull means Brevitas can transparently proxy the tool's traffic
	// by rewriting documented configuration.
	SupportFull Support = "supported"
	// SupportPartial means proxying works but with caveats (e.g. requires a
	// manual restart, or only covers one of several transports).
	SupportPartial Support = "partial"
	// SupportUnsupported means the tool cannot be proxied without violating
	// the constraints (no binary patching, no auth hacks, no code injection).
	// The Reason field on Status explains why.
	SupportUnsupported Support = "unsupported"
)

// State describes the live configuration state of a provider on this host.
type State string

const (
	StateNotDetected State = "not-detected"
	StateDetected    State = "detected"    // installed but not pointed at Brevitas
	StateConfigured  State = "configured"  // pointed at Brevitas
	StateError       State = "error"       // detected but validation failed
	StateUnsupported State = "unsupported" // detected but cannot be proxied
)

// Status is a snapshot of a provider on the current machine.
type Status struct {
	Name       string  `json:"name"`
	Support    Support `json:"support"`
	State      State   `json:"state"`
	Detected   bool    `json:"detected"`
	Configured bool    `json:"configured"`
	// ConfigPath is the primary config file Brevitas manages (if any).
	ConfigPath string `json:"config_path,omitempty"`
	// Reason explains an unsupported or error state in user-facing language.
	Reason string `json:"reason,omitempty"`
}

// Provider is implemented by every AI-tool integration.
type Provider interface {
	// Name is the stable, lowercase identifier (e.g. "claude", "cursor").
	Name() string
	// DisplayName is the human-facing label (e.g. "Claude Code").
	DisplayName() string
	// Support reports whether this tool can be proxied at all.
	Support() Support
	// Detect performs best-effort detection of the tool on this host.
	Detect(ctx context.Context) bool
	// Install rewrites the tool's configuration to route through Brevitas,
	// backing up any file it changes. It must be idempotent.
	Install(ctx context.Context) error
	// Uninstall restores the tool's original configuration from backups.
	Uninstall(ctx context.Context) error
	// Validate checks that the tool is correctly pointed at Brevitas.
	Validate(ctx context.Context) error
	// Status returns a current snapshot.
	Status(ctx context.Context) Status
}

// APIKeyFunc returns the single Brevitas API key, typically backed by the OS
// credential store.
type APIKeyFunc func(ctx context.Context) (string, error)

// Env carries injected dependencies shared by all providers.
type Env struct {
	Config *config.Config
	Dirs   config.Dirs
	Logger *slog.Logger

	// APIKey returns the single Brevitas API key that tools authenticate to
	// the local proxy with. It is sourced from the OS credential store so the
	// keyring package need not be imported here (avoids a dependency cycle and
	// keeps providers testable).
	APIKey APIKeyFunc

	// ProxyURL is the loopback URL tools are pointed at.
	ProxyURL string
}

// Log returns a provider-scoped logger, never nil.
func (e *Env) Log() *slog.Logger {
	if e != nil && e.Logger != nil {
		return e.Logger
	}
	return slog.Default()
}

// Factory constructs a Provider from an injected Env.
type Factory func(env *Env) Provider

// StatusFor derives a Status from detection/configuration booleans, applying
// consistent State semantics across every provider.
func StatusFor(name string, support Support, detected, configured bool, configPath, reason string) Status {
	st := StateNotDetected
	switch {
	case !detected:
		st = StateNotDetected
	case support == SupportUnsupported:
		st = StateUnsupported
	case configured:
		st = StateConfigured
	default:
		st = StateDetected
	}
	return Status{
		Name:       name,
		Support:    support,
		State:      st,
		Detected:   detected,
		Configured: configured,
		ConfigPath: configPath,
		Reason:     reason,
	}
}
