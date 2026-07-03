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

	"github.com/Brevitas-ai/brevitas/internal/config"
)

// State is the lifecycle state of the background service.
type State string

const (
	StateRunning      State = "running"
	StateStopped      State = "stopped"
	StateNotInstalled State = "not-installed"
	StateUnknown      State = "unknown"
)

// Reverse-DNS service identifiers used across platforms.
const (
	LabelProxy     = "com.brevitas.proxy"
	LabelOptimizer = "com.brevitas.optimizer"
)

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

// Spec describes how to run a managed service process.
type Spec struct {
	// Name is a short identifier used for file names (e.g. "proxy").
	Name string
	// Label is the reverse-DNS identifier (launchd/Task Scheduler).
	Label string
	// Description is a human-readable label (systemd Description, etc.).
	Description string
	// Executable is the absolute path to the brevitas binary.
	Executable string
	// Args are the arguments that start this service (e.g. ["serve"]).
	Args []string
	// Dirs provides log locations.
	Dirs config.Dirs
}

// ProxySpec builds the spec for the local optimization proxy (`brevitas serve`).
func ProxySpec(dirs config.Dirs) (Spec, error) {
	return specFor("proxy", LabelProxy, "Brevitas local optimization proxy", []string{"serve"}, dirs)
}

// OptimizerSpec builds the spec for the brevitas-systems optimizer adapter
// (`brevitas optimizer`) — the optimization brain the proxy calls.
func OptimizerSpec(dirs config.Dirs) (Spec, error) {
	return specFor("optimizer", LabelOptimizer, "Brevitas optimizer (brevitas-systems)", []string{"optimizer"}, dirs)
}

// DefaultSpec is retained for compatibility and returns the proxy spec.
func DefaultSpec(dirs config.Dirs) (Spec, error) { return ProxySpec(dirs) }

func specFor(name, label, desc string, args []string, dirs config.Dirs) (Spec, error) {
	exe, err := os.Executable()
	if err != nil {
		return Spec{}, err
	}
	return Spec{
		Name:        name,
		Label:       label,
		Description: desc,
		Executable:  exe,
		Args:        args,
		Dirs:        dirs,
	}, nil
}

// NewManager returns the platform-appropriate Manager.
func NewManager(spec Spec) Manager { return newManager(spec) }
