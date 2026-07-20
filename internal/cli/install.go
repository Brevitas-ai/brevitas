package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/Brevitas-ai/brevitas/internal/provider"
)

// cmdInstall dispatches between the two install paths:
//
//	bvx install ai            configure detected AI coding tools (Claude, Codex, ...)
//	bvx install repo          choose a codebase with the guided directory navigator
//	bvx install <repo>        scan a codebase and wire Brevitas into its agents
//	bvx install               (no target) defaults to "ai" for backward compatibility
func (a *App) cmdInstall(ctx context.Context, args []string) error {
	// Separate the first positional (the target) from flags, so that both
	// `bvx install ai --no-service` and `bvx install --no-service` work.
	var target string
	var rest []string
	for _, arg := range args {
		if target == "" && !strings.HasPrefix(arg, "-") {
			target = arg
			continue
		}
		rest = append(rest, arg)
	}

	switch {
	case target == "" || target == "ai":
		return a.installAITools(ctx, rest)
	case target == "repo":
		if helpRequested(rest) {
			a.printRepositoryNavigatorHelp()
			return nil
		}
		repo, selected, err := a.selectRepository()
		if err != nil {
			return fmt.Errorf("select repository: %w", err)
		}
		if !selected {
			if a.dashboardActive {
				a.returnHomeRequested = true
			}
			a.say("Installation cancelled.")
			return nil
		}
		return a.installCodebase(ctx, repo, rest)
	default:
		return a.installCodebase(ctx, target, rest)
	}
}

// installAITools runs the end-to-end AI-coding-tool installation flow.
func (a *App) installAITools(ctx context.Context, args []string) error {
	if helpRequested(args) {
		a.printInstallAIHelp()
		return nil
	}
	fs := flag.NewFlagSet("install ai", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	apiKeyFlag := fs.String("api-key", "", "Brevitas API key (for CI; otherwise browser login)")
	noService := fs.Bool("no-service", false, "configure tools but do not install the background service")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := a.Dirs.EnsureAll(); err != nil {
		return fmt.Errorf("prepare directories: %w", err)
	}

	a.page("Install AI tools", "Detect, connect, configure, and verify your local AI clients.")

	// 1. Authenticate first so a fresh install always guides the user through
	// login, even when no supported AI clients have been installed yet.
	a.section("Connecting your account")
	if err := a.ensureAPIKey(ctx, *apiKeyFlag); err != nil {
		return err
	}

	// 2. Scan.
	a.section("Scanning this machine")
	reg := a.registry()
	loading := a.startLoading("Detecting installed AI tools…")
	detected := reg.Detected(ctx)
	loading.Stop()

	var supported, manual, unsupported []provider.Provider
	for _, p := range detected {
		switch p.Support() {
		case provider.SupportFull:
			supported = append(supported, p)
			a.ok("%s", p.DisplayName())
		case provider.SupportPartial:
			manual = append(manual, p)
			a.warn("%s (manual step required)", p.DisplayName())
		case provider.SupportUnsupported:
			unsupported = append(unsupported, p)
			a.warn("%s — Unsupported", p.DisplayName())
		}
	}
	for _, p := range unsupported {
		a.say("      %s", p.Status(ctx).Reason)
	}
	a.section("Detection summary")
	a.metric("Configurable", fmt.Sprintf("%d tools", len(supported)), ansiGreen)
	a.metric("Manual setup", fmt.Sprintf("%d tools", len(manual)), ansiYellow)
	a.metric("Unsupported", fmt.Sprintf("%d tools", len(unsupported)), ansiRed)

	if len(supported)+len(manual) == 0 {
		a.note("No configurable AI tools detected. Install one, then rerun `bvx install ai`.")
		return nil
	}

	// 3. Configure.
	a.section("Installing")
	// Rebuild registry so providers pick up the freshly stored key.
	reg = a.registry()
	var configured []provider.Provider
	for _, name := range providerNames(supported) {
		p := reg.Get(name)
		if err := a.withLoading("Configuring "+p.DisplayName()+"…", func() error {
			return p.Install(ctx)
		}); err != nil {
			a.fail("%s: %v", p.DisplayName(), err)
			continue
		}
		a.ok("%s configured", p.DisplayName())
		a.Cfg.AddProvider(p.Name())
		configured = append(configured, p)
	}

	// Manual-step providers: surface instructions, do not fail.
	for _, name := range providerNames(manual) {
		p := reg.Get(name)
		err := a.withLoading("Checking "+p.DisplayName()+" setup…", func() error {
			return p.Install(ctx)
		})
		if m, ok := provider.IsManualStep(err); ok {
			a.warn("%s: %s", p.DisplayName(), m.Instructions)
			continue
		}
		if err != nil {
			a.fail("%s: %v", p.DisplayName(), err)
			continue
		}
		a.ok("%s configured", p.DisplayName())
		a.Cfg.AddProvider(p.Name())
	}

	if err := a.Cfg.Save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// 4. Background services (proxy + optimizer brain).
	if !*noService {
		a.installServices(ctx)
	}

	// 5. Diagnostics.
	a.section("Verifying installation")
	for _, p := range configured {
		if err := a.withLoading("Verifying "+p.DisplayName()+"…", func() error {
			return p.Validate(ctx)
		}); err != nil {
			a.fail("%s: %v", p.DisplayName(), err)
		} else {
			a.ok("%s", p.DisplayName())
		}
	}

	a.success("Installation complete")
	if len(manual) > 0 {
		a.note("Some tools need a one-time manual step (shown above with ⚠).")
	}
	return nil
}

// ensureAPIKey stores an explicitly supplied key or authorizes through the dashboard.
func (a *App) ensureAPIKey(ctx context.Context, provided string) error {
	if provided == "" {
		loading := a.startLoading("Checking for an existing API key…")
		hasKey := a.hasKey(ctx)
		loading.Stop()
		if hasKey {
			a.ok("Using existing API key from %s", a.Keyring.Backend())
			return nil
		}
	}
	if provided != "" {
		return a.storeAPIKey(ctx, provided)
	}
	return a.loginWithBrowser(ctx, true)
}

func providerNames(ps []provider.Provider) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name())
	}
	return out
}
