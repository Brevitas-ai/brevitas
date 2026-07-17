package cli

import (
	"context"
	"flag"

	"github.com/Brevitas-ai/brevitas/internal/keyring"
)

// cmdUninstall restores every managed config, removes the service, and
// optionally purges the stored API key.
func (a *App) cmdUninstall(ctx context.Context, args []string) error {
	if helpRequested(args) {
		a.printUninstallHelp()
		return nil
	}
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	purge := fs.Bool("purge", false, "also delete the stored API key")
	if err := fs.Parse(args); err != nil {
		return err
	}

	a.page("Uninstall", "Restore managed configurations and remove background services.")
	a.section("Restoring tools")

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

	// 2. Stop and remove the background services (proxy + optimizer).
	if svcs, err := a.services(); err == nil {
		a.section("Removing services")
		for _, s := range svcs {
			if err := s.mgr.Uninstall(ctx); err != nil {
				a.fail("%s service: %v", s.name, err)
			} else {
				a.ok("%s service removed", s.name)
			}
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
		a.note("Your API key is still stored. Use `bvx uninstall --purge` to remove it.")
	}

	a.success("Uninstall complete")
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
