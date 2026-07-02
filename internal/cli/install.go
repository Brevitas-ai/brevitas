package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/brevitas-systems/brevitas/internal/provider"
	"github.com/brevitas-systems/brevitas/internal/service"
)

// cmdInstall runs the end-to-end installation flow.
func (a *App) cmdInstall(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
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
		a.say("No configurable AI tools detected. Install one, then re-run 'brevitas install'.")
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

	// 4. Background service.
	if !*noService {
		if err := a.installService(ctx); err != nil {
			a.fail("background service: %v", err)
		} else {
			a.ok("Background service installed")
			a.ok("Proxy started")
		}
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

// installService installs and starts the background proxy service.
func (a *App) installService(ctx context.Context) error {
	spec, err := service.DefaultSpec(a.Dirs)
	if err != nil {
		return err
	}
	mgr := service.NewManager(spec)
	if err := mgr.Install(ctx); err != nil {
		return err
	}
	return mgr.Start(ctx)
}

func providerNames(ps []provider.Provider) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name())
	}
	return out
}
