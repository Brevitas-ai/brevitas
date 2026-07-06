package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/Brevitas-ai/brevitas/internal/provider"
)

// cmdInstall dispatches between the two install paths:
//
//	bvx install ai            configure detected AI coding tools (Claude, Codex, ...)
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
	default:
		return a.installCodebase(ctx, target, rest)
	}
}

// installAITools runs the end-to-end AI-coding-tool installation flow.
func (a *App) installAITools(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("install ai", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	apiKeyFlag := fs.String("api-key", "", "Brevitas API key (otherwise prompted or read from BREVITAS_API_KEY)")
	noService := fs.Bool("no-service", false, "configure tools but do not install the background service")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if err := a.Dirs.EnsureAll(); err != nil {
		return fmt.Errorf("prepare directories: %w", err)
	}

	// 1. Scan.
	a.say("Scanning system...\n")
	reg := a.registry()
	detected := reg.Detected(ctx)

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
	a.say("\nDetected %d configurable tool(s), %d manual, %d unsupported.\n",
		len(supported), len(manual), len(unsupported))

	if len(supported)+len(manual) == 0 {
		a.say("No configurable AI tools detected. Install one, then re-run 'bvx install'.")
		return nil
	}

	// 2. API key.
	if err := a.ensureAPIKey(ctx, *apiKeyFlag); err != nil {
		return err
	}

	// 3. Configure.
	a.say("\nInstalling...\n")
	// Rebuild registry so providers pick up the freshly stored key.
	reg = a.registry()
	var configured []provider.Provider
	for _, name := range providerNames(supported) {
		p := reg.Get(name)
		if err := p.Install(ctx); err != nil {
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
		err := p.Install(ctx)
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
	a.say("\nRunning diagnostics...\n")
	for _, p := range configured {
		if err := p.Validate(ctx); err != nil {
			a.fail("%s: %v", p.DisplayName(), err)
		} else {
			a.ok("%s", p.DisplayName())
		}
	}

	a.say("\nInstallation complete.")
	if len(manual) > 0 {
		a.say("Some tools need a one-time manual step (shown above with ⚠).")
	}
	return nil
}

// ensureAPIKey stores the API key, prompting if necessary.
func (a *App) ensureAPIKey(ctx context.Context, provided string) error {
	if provided == "" && a.hasKey(ctx) {
		a.say("Using existing Brevitas API key from %s.", a.Keyring.Backend())
		return nil
	}
	key := provided
	if key == "" {
		var err error
		key, err = a.promptSecret("Enter Brevitas API key: ")
		if err != nil {
			return fmt.Errorf("read api key: %w", err)
		}
	}
	if key == "" {
		return errors.New("no API key provided")
	}
	if err := a.Keyring.Set(ctx, key); err != nil {
		return fmt.Errorf("store api key in %s: %w", a.Keyring.Backend(), err)
	}
	a.ok("API key stored in %s", a.Keyring.Backend())
	return nil
}

func providerNames(ps []provider.Provider) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name())
	}
	return out
}
