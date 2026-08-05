package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
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

func (a *App) ensureOptimizerInstalled(ctx context.Context) bool {
	sys := a.systems()
	var current string
	err := a.withLoading("Checking the optimization engine…", func() error {
		var versionErr error
		current, versionErr = sys.Version(ctx)
		return versionErr
	})
	if err == nil && optimizer.CompareVersions(current, version.PinnedSystemsVersion) == 0 {
		a.ensureAgentmapInstalled(ctx, sys)
		return a.optimizerAvailable(ctx)
	}

	python := a.ensureManagedPython(ctx)
	if python == "" {
		return false
	}
	sys = optimizer.NewSystems(python)
	if err := a.withLoading(fmt.Sprintf("Installing brevitas-systems %s…", version.PinnedSystemsVersion), func() error {
		return sys.Upgrade(ctx)
	}); err != nil {
		a.fail("optimizer install: %v", err)
		return false
	}
	a.Cfg.Optimizer.PythonBin = python
	if err := a.Cfg.Save(); err != nil {
		a.warn("Could not save the managed Python path: %v", err)
	}
	a.ok("brevitas-systems %s installed", version.PinnedSystemsVersion)
	a.ensureAgentmapInstalled(ctx, sys)
	return a.optimizerAvailable(ctx)
}

// ensureManagedPython returns the interpreter of Brevitas's own virtual
// environment, creating the venv (with current pip tooling) on first use.
// Keeping packages there avoids mutating the user's Python and works with
// Homebrew's externally-managed Python installations (PEP 668). Returns ""
// after reporting a failure.
func (a *App) ensureManagedPython(ctx context.Context) string {
	venv := filepath.Join(a.Dirs.Data, "python")
	python := filepath.Join(venv, "bin", "python3")
	if runtime.GOOS == "windows" {
		python = filepath.Join(venv, "Scripts", "python.exe")
	}
	if _, statErr := os.Stat(python); os.IsNotExist(statErr) {
		if err := a.Dirs.EnsureAll(); err != nil {
			a.fail("optimizer install: %v", err)
			return ""
		}
		base := firstPython(a.Cfg.Optimizer.PythonBin,
			"python3.14", "python3.13", "python3.12", "python3.11", "python3.10",
			"python3", "python")
		if base == "" {
			a.fail("optimizer install: Python 3.10 or newer is required (brew install python)")
			return ""
		}
		var output []byte
		err := a.withLoading("Creating an isolated Python environment…", func() error {
			var commandErr error
			output, commandErr = exec.CommandContext(ctx, base, "-m", "venv", venv).CombinedOutput()
			return commandErr
		})
		if err != nil {
			a.fail("optimizer install: create Python environment: %v: %s", err, output)
			return ""
		}
	}
	var output []byte
	err := a.withLoading("Preparing secure Python tooling…", func() error {
		var commandErr error
		output, commandErr = exec.CommandContext(ctx, python, "-m", "pip", "install", "--upgrade", "pip", "setuptools").CombinedOutput()
		return commandErr
	})
	if err != nil {
		a.fail("optimizer install: secure Python tooling: %v: %s", err, output)
		return ""
	}
	return python
}

// ensureAgentmapInstalled installs the pinned agentmap-scan scanner into the
// same interpreter as brevitas-systems so `bvx install <repo>` works out of
// the box. Failure is non-fatal: the codebase command falls back to printing
// pip instructions.
func (a *App) ensureAgentmapInstalled(ctx context.Context, sys *optimizer.Systems) {
	if v, err := sys.PackageVersion(ctx, "agentmap-scan"); err == nil &&
		optimizer.CompareVersions(v, version.PinnedAgentmapVersion) >= 0 {
		return
	}
	if err := a.withLoading(fmt.Sprintf("Installing the codebase scanner (agentmap-scan %s)…", version.PinnedAgentmapVersion), func() error {
		return sys.InstallPackage(ctx, version.AgentmapPipSpec())
	}); err != nil {
		a.warn("Codebase scanner not installed: %v", err)
		return
	}
	a.ok("agentmap-scan %s installed", version.PinnedAgentmapVersion)
}

// firstPython returns the first candidate on PATH that is Python 3.10 or
// newer — the floor shared by brevitas-systems and agentmap-scan. macOS ships
// a bare 3.9 at /usr/bin/python3, which would create a venv no pinned package
// can install into.
func firstPython(candidates ...string) string {
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		path, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		if exec.Command(path, "-c", "import sys; sys.exit(0 if sys.version_info >= (3, 10) else 1)").Run() == nil {
			return path
		}
	}
	return ""
}

// ensureStarted refreshes the service definition so upgrades cannot keep an
// older executable path, then starts it.
func (a *App) ensureStarted(ctx context.Context, mgr service.Manager) error {
	if err := mgr.Install(ctx); err != nil {
		return err
	}
	return mgr.Start(ctx)
}

// installServices installs the pinned Python brain, then starts both services.
func (a *App) installServices(ctx context.Context) {
	pm, err := a.proxyManager()
	if err != nil {
		a.fail("proxy service: %v", err)
	} else if err := a.withLoading("Installing and starting the proxy…", func() error {
		return a.ensureStarted(ctx, pm)
	}); err != nil {
		a.fail("proxy service: %v", err)
	} else {
		a.ok("Background service installed")
		a.ok("Proxy started")
	}

	om, err := a.optimizerManager()
	if err != nil {
		return
	}
	if a.ensureOptimizerInstalled(ctx) {
		if err := a.withLoading("Starting the optimization engine…", func() error {
			return a.ensureStarted(ctx, om)
		}); err != nil {
			a.fail("optimizer service: %v", err)
		} else {
			a.ok("Optimizer started (brevitas-systems)")
		}
	} else {
		a.warn("Optimizer not started; the proxy will forward requests unchanged.")
	}
}

func (a *App) cmdStart(ctx context.Context, _ []string) error {
	a.page("Start services", "Launch the BVX proxy and optimization engine.")
	a.section("Services")
	pm, err := a.proxyManager()
	if err != nil {
		return err
	}
	if err := a.withLoading("Starting the proxy…", func() error {
		return a.ensureStarted(ctx, pm)
	}); err != nil {
		return err
	}
	a.ok("proxy started (%s)", pm.Backend())

	om, err := a.optimizerManager()
	if err != nil {
		return err
	}
	if a.optimizerAvailable(ctx) {
		if err := a.withLoading("Starting the optimization engine…", func() error {
			return a.ensureStarted(ctx, om)
		}); err != nil {
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
	a.page("Stop services", "Stop the BVX proxy and optimization engine.")
	a.section("Services")
	svcs, err := a.services()
	if err != nil {
		return err
	}
	for _, s := range svcs {
		if st, _ := s.mgr.Status(ctx); st == service.StateNotInstalled {
			continue
		}
		if err := a.withLoading("Stopping the "+s.name+"…", func() error {
			return s.mgr.Stop(ctx)
		}); err != nil {
			a.fail("%s: %v", s.name, err)
		} else {
			a.ok("%s stopped", s.name)
		}
	}
	return nil
}

func (a *App) cmdRestart(ctx context.Context, _ []string) error {
	a.page("Restart services", "Refresh the BVX proxy and optimization engine.")
	a.section("Services")
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
		if err := a.withLoading("Restarting the "+s.name+"…", func() error {
			if err := a.ensureStarted(ctx, s.mgr); err != nil {
				return err
			}
			return s.mgr.Restart(ctx)
		}); err != nil {
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
	go a.runInventoryHeartbeats(ctx, logger)

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
