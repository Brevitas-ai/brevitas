package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Brevitas-ai/brevitas/internal/optimizer"
	"github.com/Brevitas-ai/brevitas/internal/version"
)

// runForeground execs a command and streams its output, terminating it when
// the context is cancelled (exec.CommandContext kills the process on cancel).
func runForeground(ctx context.Context, name string, args []string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// cmdOptimizer runs the brevitas-systems optimizer adapter in the foreground.
// It auto-detects a Python interpreter that has the brevitas package, writes
// the embedded adapter script, and serves the socket the proxy dials. The
// service manager (or the user) runs this alongside `brevitas serve`.
func (a *App) cmdOptimizer(ctx context.Context, _ []string) error {
	if err := a.Dirs.EnsureAll(); err != nil {
		return err
	}

	python := optimizer.DetectPython(ctx, a.Cfg.Optimizer.PythonBin)
	if python == "" {
		return fmt.Errorf("no Python interpreter with the brevitas package found; run: pip install %s", version.SystemsPipSpec())
	}

	script, err := optimizer.WriteAdapter(a.Dirs.Data)
	if err != nil {
		return fmt.Errorf("write adapter: %w", err)
	}

	// Persist the working interpreter so doctor/update use it too.
	if a.Cfg.Optimizer.PythonBin != python {
		a.Cfg.Optimizer.PythonBin = python
		_ = a.Cfg.Save()
	}

	var args []string
	switch a.Cfg.Optimizer.Transport {
	case "tcp":
		args = []string{script, "--tcp", a.Cfg.Optimizer.Address}
	default:
		args = []string{script, "--unix", a.Cfg.Optimizer.Address}
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if os.Getenv("BREVITAS_CACHE_DB") == "" {
		_ = os.Setenv("BREVITAS_CACHE_DB", filepath.Join(a.Dirs.Data, "semantic_cache.db"))
	}

	a.say("Starting Brevitas optimizer via %s (%s)", python, a.Cfg.Optimizer.Address)
	return runForeground(ctx, python, args, a.Err)
}
