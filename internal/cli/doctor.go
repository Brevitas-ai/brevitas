package cli

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/brevitas-systems/brevitas/internal/provider"
	"github.com/brevitas-systems/brevitas/internal/service"
	"github.com/brevitas-systems/brevitas/internal/version"
)

// check is a single diagnostic result.
type check struct {
	name string
	err  error
	warn bool
}

func (a *App) cmdDoctor(ctx context.Context, _ []string) error {
	a.say("Running Brevitas diagnostics...\n")

	var checks []check
	add := func(name string, err error) { checks = append(checks, check{name: name, err: err}) }
	addWarn := func(name string, err error) { checks = append(checks, check{name: name, err: err, warn: true}) }

	// Service health.
	if mgr, err := a.manager(); err != nil {
		add("service manager", err)
	} else {
		st, serr := mgr.Status(ctx)
		if serr != nil {
			add("service status", serr)
		} else if st != service.StateRunning {
			addWarn("service running", fmt.Errorf("state is %q", st))
		} else {
			add("service running", nil)
		}
	}

	// Proxy alive.
	add("proxy reachable", a.proxyHealth(ctx))

	// API key.
	if a.hasKey(ctx) {
		add("api key present", nil)
	} else {
		add("api key present", fmt.Errorf("no key stored; run 'brevitas login'"))
	}

	// Network to upstreams.
	for family, base := range a.Cfg.Upstreams {
		addWarn("network: "+family, a.reachable(ctx, base))
	}

	// brevitas-systems package + service.
	sys := a.systems()
	if v, err := sys.Version(ctx); err != nil {
		add("brevitas-systems installed", err)
	} else {
		add("brevitas-systems "+v, nil)
	}
	addWarn("brevitas-systems service", a.optimizerClient().Health(ctx))

	// Provider configs.
	for _, s := range a.registry().Statuses(ctx) {
		if !s.Detected || s.Support == provider.SupportUnsupported {
			continue
		}
		if s.Support == provider.SupportPartial {
			addWarn("config: "+s.Name, fmt.Errorf("manual step: %s", s.Reason))
			continue
		}
		if s.State == provider.StateConfigured {
			add("config: "+s.Name, nil)
		} else {
			addWarn("config: "+s.Name, fmt.Errorf("detected but not configured"))
		}
	}

	// Render.
	var failed int
	for _, c := range checks {
		switch {
		case c.err == nil:
			a.ok("%s", c.name)
		case c.warn:
			a.warn("%s: %v", c.name, c.err)
		default:
			a.fail("%s: %v", c.name, c.err)
			failed++
		}
	}

	a.say("\n%s", version.String())
	if failed > 0 {
		return fmt.Errorf("%d diagnostic(s) failed", failed)
	}
	a.say("\nAll critical checks passed.")
	return nil
}

// proxyHealth checks the local proxy's health endpoint.
func (a *App) proxyHealth(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.Cfg.ProxyURL()+"/__brevitas/health", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health status %s", resp.Status)
	}
	return nil
}

// reachable performs a lightweight TCP/HTTP reachability probe to a base URL.
func (a *App) reachable(ctx context.Context, base string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, base, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}
