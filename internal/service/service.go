// Package service installs and controls the Brevitas proxy as a per-user
// background service:
//
//	macOS   -> launchd LaunchAgent
//	Linux   -> systemd --user unit
//	Windows -> Task Scheduler logon task (see service_windows.go for rationale)
//
// It exposes install/uninstall/start/stop/restart/status, all context-aware.
package service

import (
	"context"
	"os"

	"github.com/brevitas-systems/brevitas/internal/config"
)

// State is the lifecycle state of the background service.
type State string

const (
	StateRunning      State = "running"
	StateStopped      State = "stopped"
	StateNotInstalled State = "not-installed"
	StateUnknown      State = "unknown"
)

// Label is the reverse-DNS service identifier used across platforms.
const Label = "com.brevitas.proxy"

// Manager installs and controls the background service.
type Manager interface {
	// Install registers the service definition (idempotent).
	Install(ctx context.Context) error
	// Uninstall stops and removes the service definition.
	Uninstall(ctx context.Context) error
	// Start launches the service.
	Start(ctx context.Context) error
	// Stop halts the service.
	Stop(ctx context.Context) error
	// Restart stops then starts the service.
	Restart(ctx context.Context) error
	// Status reports the current lifecycle state.
	Status(ctx context.Context) (State, error)
	// Backend returns a human-readable backend name.
	Backend() string
}

// Spec describes how to run the service process.
type Spec struct {
	// Executable is the absolute path to the brevitas binary.
	Executable string
	// Args are the arguments that start the proxy (e.g. ["serve"]).
	Args []string
	// Dirs provides log locations.
	Dirs config.Dirs
}

// DefaultSpec builds a Spec that runs `brevitas serve` using the current
// executable path.
func DefaultSpec(dirs config.Dirs) (Spec, error) {
	exe, err := os.Executable()
	if err != nil {
		return Spec{}, err
	}
	return Spec{
		Executable: exe,
		Args:       []string{"serve"},
		Dirs:       dirs,
	}, nil
}

// NewManager returns the platform-appropriate Manager.
func NewManager(spec Spec) Manager { return newManager(spec) }
