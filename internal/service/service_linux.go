//go:build linux

package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

type systemdManager struct {
	spec     Spec
	unitName string
	unitPath string
}

func newManager(spec Spec) Manager {
	home, _ := os.UserHomeDir()
	unit := "brevitas.service"
	return &systemdManager{
		spec:     spec,
		unitName: unit,
		unitPath: filepath.Join(home, ".config", "systemd", "user", unit),
	}
}

func (m *systemdManager) Backend() string { return "systemd (--user unit)" }

const unitTemplate = `[Unit]
Description=Brevitas local optimization proxy
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart={{.ExecStart}}
Restart=on-failure
RestartSec=2
StandardOutput=append:{{.StdoutPath}}
StandardError=append:{{.StderrPath}}

[Install]
WantedBy=default.target
`

func (m *systemdManager) Install(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(m.unitPath), 0o755); err != nil {
		return err
	}
	if err := m.spec.Dirs.EnsureAll(); err != nil {
		return err
	}

	execStart := m.spec.Executable
	for _, a := range m.spec.Args {
		execStart += " " + a
	}

	tmpl := template.Must(template.New("unit").Parse(unitTemplate))
	var buf bytes.Buffer
	data := map[string]any{
		"ExecStart":  execStart,
		"StdoutPath": filepath.Join(m.spec.Dirs.Logs, "proxy.out.log"),
		"StderrPath": filepath.Join(m.spec.Dirs.Logs, "proxy.err.log"),
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}
	if err := os.WriteFile(m.unitPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write unit: %w", err)
	}

	if err := m.run(ctx, "daemon-reload"); err != nil {
		return err
	}
	return m.run(ctx, "enable", "--now", m.unitName)
}

func (m *systemdManager) Uninstall(ctx context.Context) error {
	_ = m.run(ctx, "disable", "--now", m.unitName)
	if err := os.Remove(m.unitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return m.run(ctx, "daemon-reload")
}

func (m *systemdManager) Start(ctx context.Context) error   { return m.run(ctx, "start", m.unitName) }
func (m *systemdManager) Stop(ctx context.Context) error    { return m.run(ctx, "stop", m.unitName) }
func (m *systemdManager) Restart(ctx context.Context) error { return m.run(ctx, "restart", m.unitName) }

func (m *systemdManager) Status(ctx context.Context) (State, error) {
	if _, err := os.Stat(m.unitPath); os.IsNotExist(err) {
		return StateNotInstalled, nil
	}
	out, _ := m.runOut(ctx, "is-active", m.unitName)
	switch strings.TrimSpace(out) {
	case "active":
		return StateRunning, nil
	case "inactive", "failed":
		return StateStopped, nil
	default:
		return StateUnknown, nil
	}
}

func (m *systemdManager) run(ctx context.Context, args ...string) error {
	_, err := m.runOut(ctx, args...)
	return err
}

func (m *systemdManager) runOut(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"--user"}, args...)
	cmd := exec.CommandContext(ctx, "systemctl", full...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}
