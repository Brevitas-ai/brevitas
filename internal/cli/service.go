package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Brevitas-ai/brevitas/internal/logging"
	"github.com/Brevitas-ai/brevitas/internal/optimizer"
	"github.com/Brevitas-ai/brevitas/internal/proxy"
	"github.com/Brevitas-ai/brevitas/internal/service"
	"github.com/Brevitas-ai/brevitas/internal/version"
)

// managedService pairs a service with its display name.
type managedService struct {
	name string
	mgr  service.Manager
}

// proxyManager returns the manager for the proxy service.
func (a *App) proxyManager() (service.Manager, error) {
	spec, err := service.ProxySpec(a.Dirs)
	if err != nil {
		return nil, err
	}
	return service.NewManager(spec), nil
}

// optimizerManager returns the manager for the optimizer (brain) service.
func (a *App) optimizerManager() (service.Manager, error) {
	spec, err := service.OptimizerSpec(a.Dirs)
	if err != nil {
		return nil, err
	}
	return service.NewManager(spec), nil
}

// manager returns the proxy manager (kept for status/doctor callers).
func (a *App) manager() (service.Manager, error) { return a.proxyManager() }

// services returns both managed services, proxy first.
func (a *App) services() ([]managedService, error) {
	pm, err := a.proxyManager()
	if err != nil {
		return nil, err
	}
	om, err := a.optimizerManager()
	if err != nil {
		return nil, err
	}
	return []managedService{{"proxy", pm}, {"optimizer", om}}, nil
}

// optimizerAvailable reports whether a Python with the brevitas package exists.
// When found, it persists the resolved ABSOLUTE interpreter path to config so
// the background service (which runs with a minimal PATH) uses the exact same
// interpreter as the interactive shell — critical for conda/Homebrew installs.
func (a *App) optimizerAvailable(ctx context.Context) bool {
	py := optimizer.DetectPython(ctx, a.Cfg.Optimizer.PythonBin)
	if py == "" {
		return false
	}
	if py != a.Cfg.Optimizer.PythonBin {
		a.Cfg.Optimizer.PythonBin = py
		_ = a.Cfg.Save()
	}
	return true
}

// ensureStarted installs the service if needed, then starts it.
func (a *App) ensureStarted(ctx context.Context, mgr service.Manager) error {
	if st, _ := mgr.Status(ctx); st == service.StateNotInstalled {
		if err := mgr.Install(ctx); err != nil {
			return err
		}
	}
	return mgr.Start(ctx)
}

// installServices installs and starts the proxy, plus the optimizer when the
// brevitas package is available. It prints a line per service.
func (a *App) installServices(ctx context.Context) {
	pm, err := a.proxyManager()
	if err != nil {
		a.fail("proxy service: %v", err)
	} else if err := a.ensureStarted(ctx, pm); err != nil {
		a.fail("proxy service: %v", err)
	} else {
		a.ok("Background service installed")
		a.ok("Proxy started")
	}

	om, err := a.optimizerManager()
	if err != nil {
		return
	}
	if a.optimizerAvailable(ctx) {
		if err := a.ensureStarted(ctx, om); err != nil {
			a.fail("optimizer service: %v", err)
		} else {
			a.ok("Optimizer started (brevitas-systems)")
		}
	} else {
		a.warn("Optimizer not started — brevitas-systems not found.")
		a.warn("  Run: pip install %s && bvx repair", version.SystemsPipSpec())
	}
}

func (a *App) cmdStart(ctx context.Context, _ []string) error {
	pm, err := a.proxyManager()
	if err != nil {
		return err
	}
	if err := a.ensureStarted(ctx, pm); err != nil {
		return err
	}
	a.ok("proxy started (%s)", pm.Backend())

	om, err := a.optimizerManager()
	if err != nil {
		return err
	}
	if a.optimizerAvailable(ctx) {
		if err := a.ensureStarted(ctx, om); err != nil {
			a.fail("optimizer: %v", err)
		} else {
			a.ok("optimizer started")
		}
	} else {
		a.warn("optimizer not started — brevitas-systems not found (pip install %s)", version.SystemsPipSpec())
	}
	return nil
}

func (a *App) cmdStop(ctx context.Context, _ []string) error {
	svcs, err := a.services()
	if err != nil {
		return err
	}
	for _, s := range svcs {
		if st, _ := s.mgr.Status(ctx); st == service.StateNotInstalled {
			continue
		}
		if err := s.mgr.Stop(ctx); err != nil {
			a.fail("%s: %v", s.name, err)
		} else {
			a.ok("%s stopped", s.name)
		}
	}
	return nil
}

func (a *App) cmdRestart(ctx context.Context, _ []string) error {
	svcs, err := a.services()
	if err != nil {
		return err
	}
	for _, s := range svcs {
		// Skip the optimizer entirely when its runtime isn't available.
		if s.name == "optimizer" && !a.optimizerAvailable(ctx) {
			a.warn("optimizer skipped — brevitas-systems not found")
			continue
		}
		if err := a.ensureStarted(ctx, s.mgr); err != nil {
			a.fail("%s: %v", s.name, err)
			continue
		}
		if err := s.mgr.Restart(ctx); err != nil {
			a.fail("%s: %v", s.name, err)
		} else {
			a.ok("%s restarted", s.name)
		}
	}
	return nil
}

// cmdServe runs the proxy in the foreground. The service manager invokes this;
// users normally use start/stop/restart instead.
func (a *App) cmdServe(ctx context.Context, _ []string) error {
	if err := a.Dirs.EnsureAll(); err != nil {
		return err
	}

	logger := logging.Init(logging.Options{
		Level:  logging.ParseLevel(os.Getenv("BREVITAS_LOG_LEVEL")),
		Format: logging.FormatJSON,
		Output: logFileOr(a.Dirs.ProxyLog(), a.Err),
	})

	srv := proxy.New(proxy.Options{
		Config:    a.Cfg,
		Optimizer: a.optimizerClient(),
		APIKey:    proxy.APIKeyFunc(a.apiKeyFunc()),
		Logger:    logger,
	})

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("Brevitas proxy starting", "addr", a.Cfg.Addr())
	if err := srv.ListenAndServe(ctx); err != nil {
		return fmt.Errorf("proxy: %w", err)
	}
	return nil
}

// logFileOr opens the log file for appending, falling back to a writer.
func logFileOr(path string, fallback interface {
	Write([]byte) (int, error)
}) *os.File {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		if osf, ok := fallback.(*os.File); ok {
			return osf
		}
		return os.Stderr
	}
	return f
}
