package cli

import (
	"context"
	"flag"

	"github.com/brevitas-systems/brevitas/internal/keyring"
	"github.com/brevitas-systems/brevitas/internal/service"
)

// cmdUninstall restores every managed config, removes the service, and
// optionally purges the stored API key.
func (a *App) cmdUninstall(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	purge := fs.Bool("purge", false, "also delete the stored API key")
	if err := fs.Parse(args); err != nil {
		return err
	}

	a.say("Uninstalling Brevitas...\n")

	// 1. Restore provider configs from backups.
	reg := a.registry()
	for _, p := range reg.All() {
		if err := p.Uninstall(ctx); err != nil {
			a.fail("%s: %v", p.DisplayName(), err)
			continue
		}
		if contains(a.Cfg.EnabledProviders, p.Name()) {
			a.ok("%s restored", p.DisplayName())
		}
	}

	// 2. Stop and remove the service.
	if spec, err := service.DefaultSpec(a.Dirs); err == nil {
		mgr := service.NewManager(spec)
		if err := mgr.Uninstall(ctx); err != nil {
			a.fail("service: %v", err)
		} else {
			a.ok("background service removed")
		}
	}

	// 3. Clear enabled provider list.
	a.Cfg.EnabledProviders = nil
	if err := a.Cfg.Save(); err != nil {
		a.warn("could not update config: %v", err)
	}

	// 4. Optionally purge the API key.
	if *purge {
		err := a.Keyring.Delete(ctx)
		if err != nil && err != keyring.ErrNotFound {
			a.fail("remove api key: %v", err)
		} else {
			a.ok("API key removed from %s", a.Keyring.Backend())
		}
	} else {
		a.say("\nYour API key is still stored (use --purge to remove it).")
	}

	a.say("\nUninstall complete.")
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
