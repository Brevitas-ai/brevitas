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
			loading := a.startLoading("Checking the " + s.name + " service…")
			st, _ := s.mgr.Status(ctx)
			loading.Stop()
			a.statusLine("Service: "+s.name, string(st), st == service.StateRunning)
		}
	}

	// Proxy reachability.
	if err := a.withLoading("Checking proxy connectivity…", func() error {
		return a.proxyHealth(ctx)
	}); err == nil {
		a.statusLine("Proxy", a.Cfg.ProxyURL(), true)
	} else {
		a.statusLine("Proxy", "unreachable", false)
	}

	// API key.
	loading := a.startLoading("Checking stored credentials…")
	hasKey := a.hasKey(ctx)
	loading.Stop()
	a.statusLine("API key", a.Keyring.Backend(), hasKey)
	if a.Cfg.Inventory.DeviceID != "" {
		detail := fmt.Sprintf("%d registered installation(s)", len(a.Cfg.Inventory.Installations))
		a.statusLine("AgentMap inventory", detail, true)
	}

	// Optimizer.
	if err := a.withLoading("Checking the optimization engine…", func() error {
		return a.optimizerClient().Health(ctx)
	}); err == nil {
		a.statusLine("brevitas-systems", "reachable", true)
	} else {
		a.statusLine("brevitas-systems", "not running", false)
	}

	// Providers.
	a.section("Configured tools")
	any := false
	loading = a.startLoading("Inspecting AI tool configurations…")
	statuses := a.registry().Statuses(ctx)
	loading.Stop()
	for _, s := range statuses {
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
