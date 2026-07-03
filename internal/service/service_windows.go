//go:build windows

package service

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// On Windows the background service is implemented as a per-user Task
// Scheduler logon task rather than a raw SCM Windows Service.
//
// Rationale: a true SCM service requires the executable to implement the
// Service Control Protocol (via golang.org/x/sys/windows/svc), which would add
// an external dependency and a service-mode entrypoint. Task Scheduler is a
// first-class, documented Windows mechanism that runs `brevitas serve` at
// logon, restarts it on failure, and supports start/stop/status — meeting the
// same requirements without binary shims or SCM plumbing. It is fully
// reversible via schtasks /Delete.
type schtasksManager struct {
	spec     Spec
	taskName string
}

func newManager(spec Spec) Manager {
	return &schtasksManager{spec: spec, taskName: spec.Label}
}

func (m *schtasksManager) Backend() string { return "Windows Task Scheduler (logon task)" }

func (m *schtasksManager) command() string {
	parts := append([]string{quote(m.spec.Executable)}, m.spec.Args...)
	return strings.Join(parts, " ")
}

func (m *schtasksManager) Install(ctx context.Context) error {
	if err := m.spec.Dirs.EnsureAll(); err != nil {
		return err
	}
	// /F overwrites an existing task, making install idempotent.
	_, err := m.runOut(ctx,
		"/Create", "/TN", m.taskName, "/SC", "ONLOGON", "/RL", "LIMITED",
		"/TR", m.command(), "/F",
	)
	return err
}

func (m *schtasksManager) Uninstall(ctx context.Context) error {
	_ = m.Stop(ctx)
	_, err := m.runOut(ctx, "/Delete", "/TN", m.taskName, "/F")
	if err != nil && strings.Contains(err.Error(), "cannot find") {
		return nil
	}
	return err
}

func (m *schtasksManager) Start(ctx context.Context) error {
	_, err := m.runOut(ctx, "/Run", "/TN", m.taskName)
	return err
}

func (m *schtasksManager) Stop(ctx context.Context) error {
	_, err := m.runOut(ctx, "/End", "/TN", m.taskName)
	return err
}

func (m *schtasksManager) Restart(ctx context.Context) error {
	_ = m.Stop(ctx)
	return m.Start(ctx)
}

func (m *schtasksManager) Status(ctx context.Context) (State, error) {
	out, err := m.runOut(ctx, "/Query", "/TN", m.taskName, "/FO", "LIST")
	if err != nil {
		if strings.Contains(strings.ToLower(out+err.Error()), "cannot find") {
			return StateNotInstalled, nil
		}
		return StateUnknown, err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Status:") {
			status := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Status:"))
			switch strings.ToLower(status) {
			case "running":
				return StateRunning, nil
			case "ready", "disabled":
				return StateStopped, nil
			}
		}
	}
	return StateStopped, nil
}

func (m *schtasksManager) runOut(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "schtasks", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	trimmed := strings.TrimSpace(out.String())
	if err != nil {
		return trimmed, fmt.Errorf("schtasks %s: %v: %s", strings.Join(args, " "), err, trimmed)
	}
	return trimmed, nil
}

func quote(s string) string {
	if strings.ContainsAny(s, " \t") {
		return `"` + s + `"`
	}
	return s
}
