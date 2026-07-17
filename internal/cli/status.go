package cli

import (
	"context"
	"fmt"

	"github.com/Brevitas-ai/brevitas/internal/provider"
	"github.com/Brevitas-ai/brevitas/internal/service"
)

func (a *App) cmdStatus(ctx context.Context, _ []string) error {
	a.page("System status", "Services, credentials, optimizer, and configured tools.")
	a.section("Runtime")

	// Services (proxy + optimizer).
	if svcs, err := a.services(); err == nil {
		for _, s := range svcs {
			st, _ := s.mgr.Status(ctx)
			a.statusLine("Service: "+s.name, string(st), st == service.StateRunning)
		}
	}

	// Proxy reachability.
	if err := a.proxyHealth(ctx); err == nil {
		a.statusLine("Proxy", a.Cfg.ProxyURL(), true)
	} else {
		a.statusLine("Proxy", "unreachable", false)
	}

	// API key.
	a.statusLine("API key", a.Keyring.Backend(), a.hasKey(ctx))

	// Optimizer.
	if err := a.optimizerClient().Health(ctx); err == nil {
		a.statusLine("brevitas-systems", "reachable", true)
	} else {
		a.statusLine("brevitas-systems", "not running", false)
	}

	// Providers.
	a.section("Configured tools")
	any := false
	for _, s := range a.registry().Statuses(ctx) {
		if s.State == provider.StateConfigured {
			a.ok("%s", s.Name)
			any = true
		}
	}
	if !any {
		a.note("No tools configured yet. Start with `bvx install ai`.")
	}
	return nil
}

func (a *App) statusLine(label, detail string, ok bool) {
	glyph := a.styled(ansiRed+ansiBold, "✗")
	if ok {
		glyph = a.styled(ansiGreen+ansiBold, "✓")
	}
	fmt.Fprintf(a.Out, "  %s  %-20s %s\n", glyph, label, a.styled(ansiDim, detail))
}
