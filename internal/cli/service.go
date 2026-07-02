package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/brevitas-systems/brevitas/internal/logging"
	"github.com/brevitas-systems/brevitas/internal/proxy"
	"github.com/brevitas-systems/brevitas/internal/service"
)

func (a *App) manager() (service.Manager, error) {
	spec, err := service.DefaultSpec(a.Dirs)
	if err != nil {
		return nil, err
	}
	return service.NewManager(spec), nil
}

func (a *App) cmdStart(ctx context.Context, _ []string) error {
	mgr, err := a.manager()
	if err != nil {
		return err
	}
	if st, _ := mgr.Status(ctx); st == service.StateNotInstalled {
		if err := mgr.Install(ctx); err != nil {
			return err
		}
	}
	if err := mgr.Start(ctx); err != nil {
		return err
	}
	a.ok("service started (%s)", mgr.Backend())
	return nil
}

func (a *App) cmdStop(ctx context.Context, _ []string) error {
	mgr, err := a.manager()
	if err != nil {
		return err
	}
	if err := mgr.Stop(ctx); err != nil {
		return err
	}
	a.ok("service stopped")
	return nil
}

func (a *App) cmdRestart(ctx context.Context, _ []string) error {
	mgr, err := a.manager()
	if err != nil {
		return err
	}
	if err := mgr.Restart(ctx); err != nil {
		return err
	}
	a.ok("service restarted")
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

	logger.Info("brevitas proxy starting", "addr", a.Cfg.Addr())
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
