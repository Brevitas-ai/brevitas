package cli

import (
	"context"

	"github.com/Brevitas-ai/brevitas/internal/provider"
)

// cmdRepair re-applies configuration for every previously-enabled provider and
// restarts the service. Useful after a tool update rewrites its own config.
func (a *App) cmdRepair(ctx context.Context, _ []string) error {
	a.say("Repairing Brevitas installation...\n")

	if err := a.Dirs.EnsureAll(); err != nil {
		return err
	}

	reg := a.registry()
	for _, name := range a.Cfg.EnabledProviders {
		p := reg.Get(name)
		if p == nil {
			continue
		}
		if err := p.Install(ctx); err != nil {
			if _, ok := provider.IsManualStep(err); ok {
				a.warn("%s: manual step still required", p.DisplayName())
				continue
			}
			a.fail("%s: %v", p.DisplayName(), err)
			continue
		}
		if err := p.Validate(ctx); err != nil {
			a.fail("%s: %v", p.DisplayName(), err)
		} else {
			a.ok("%s re-configured", p.DisplayName())
		}
	}

	// Ensure service is installed and (re)started.
	if err := a.installService(ctx); err != nil {
		a.fail("service: %v", err)
	} else {
		a.ok("service restarted")
	}

	a.say("\nRepair complete.")
	return nil
}
