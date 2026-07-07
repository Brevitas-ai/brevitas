package cli

import (
	"context"
	"flag"
	"strings"

	"github.com/Brevitas-ai/brevitas/internal/optimizer"
	"github.com/Brevitas-ai/brevitas/internal/version"
)

// cmdUpdate checks whether the brevitas-systems package is outdated and offers
// to upgrade it. Brevitas never bundles optimization code — it only manages the
// pip-installed package.
func (a *App) cmdUpdate(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	assumeYes := fs.Bool("yes", false, "upgrade without prompting")
	fs.BoolVar(assumeYes, "y", false, "shorthand for --yes")
	if err := fs.Parse(args); err != nil {
		return err
	}

	sys := a.systems()

	current, err := sys.Version(ctx)
	if err != nil {
		a.warn("brevitas-systems is not installed: %v", err)
		if !a.confirm(*assumeYes, "Install brevitas-systems now? [y/N] ") {
			return nil
		}
		return a.doUpgrade(ctx, sys)
	}
	a.say("Installed brevitas-systems: %s", current)

	pinned := version.PinnedSystemsVersion
	if optimizer.CompareVersions(current, pinned) == 0 {
		a.ok("brevitas-systems is pinned and up to date (%s)", pinned)
		return nil
	}

	a.say("Pinned version is %s (installed %s)", pinned, current)
	if !a.confirm(*assumeYes, "Install pinned version now? [y/N] ") {
		return nil
	}
	return a.doUpgrade(ctx, sys)
}

func (a *App) doUpgrade(ctx context.Context, sys *optimizer.Systems) error {
	a.say("Upgrading brevitas-systems...")
	if err := sys.Upgrade(ctx); err != nil {
		return err
	}
	v, _ := sys.Version(ctx)
	a.ok("brevitas-systems upgraded to %s", v)
	a.say("Restart the service to pick up the new version: bvx restart")
	return nil
}

func (a *App) confirm(assumeYes bool, label string) bool {
	if assumeYes {
		return true
	}
	ans, err := a.prompt(label)
	if err != nil {
		return false
	}
	ans = strings.ToLower(strings.TrimSpace(ans))
	return ans == "y" || ans == "yes"
}
