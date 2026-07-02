//go:build darwin

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

type launchdManager struct {
	spec  Spec
	label string
	plist string
}

func newManager(spec Spec) Manager {
	home, _ := os.UserHomeDir()
	return &launchdManager{
		spec:  spec,
		label: Label,
		plist: filepath.Join(home, "Library", "LaunchAgents", Label+".plist"),
	}
}

func (m *launchdManager) Backend() string { return "launchd (LaunchAgent)" }

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.Executable}}</string>
{{range .Args}}		<string>{{.}}</string>
{{end}}	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>{{.StdoutPath}}</string>
	<key>StandardErrorPath</key>
	<string>{{.StderrPath}}</string>
	<key>ProcessType</key>
	<string>Background</string>
</dict>
</plist>
`

func (m *launchdManager) Install(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(m.plist), 0o755); err != nil {
		return err
	}
	if err := m.spec.Dirs.EnsureAll(); err != nil {
		return err
	}

	tmpl := template.Must(template.New("plist").Parse(plistTemplate))
	var buf bytes.Buffer
	data := map[string]any{
		"Label":      m.label,
		"Executable": m.spec.Executable,
		"Args":       m.spec.Args,
		"StdoutPath": filepath.Join(m.spec.Dirs.Logs, "proxy.out.log"),
		"StderrPath": filepath.Join(m.spec.Dirs.Logs, "proxy.err.log"),
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}
	if err := os.WriteFile(m.plist, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	// Reload if already loaded, then load with -w to persist across logins.
	_ = m.run(ctx, "launchctl", "unload", m.plist)
	if out, err := m.runOut(ctx, "launchctl", "load", "-w", m.plist); err != nil {
		return fmt.Errorf("launchctl load: %w: %s", err, out)
	}
	return nil
}

func (m *launchdManager) Uninstall(ctx context.Context) error {
	_ = m.run(ctx, "launchctl", "unload", "-w", m.plist)
	if err := os.Remove(m.plist); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *launchdManager) Start(ctx context.Context) error {
	if _, err := os.Stat(m.plist); os.IsNotExist(err) {
		return fmt.Errorf("service not installed")
	}
	// load is idempotent-ish; kickstart forces a (re)start if supported.
	_ = m.run(ctx, "launchctl", "load", "-w", m.plist)
	uid := fmt.Sprintf("gui/%d/%s", os.Getuid(), m.label)
	_ = m.run(ctx, "launchctl", "kickstart", "-k", uid)
	return nil
}

func (m *launchdManager) Stop(ctx context.Context) error {
	return m.run(ctx, "launchctl", "unload", m.plist)
}

func (m *launchdManager) Restart(ctx context.Context) error {
	_ = m.Stop(ctx)
	return m.Start(ctx)
}

func (m *launchdManager) Status(ctx context.Context) (State, error) {
	if _, err := os.Stat(m.plist); os.IsNotExist(err) {
		return StateNotInstalled, nil
	}
	out, err := m.runOut(ctx, "launchctl", "list")
	if err != nil {
		return StateUnknown, err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, m.label) {
			fields := strings.Fields(line)
			// Columns: PID  Status  Label. A numeric PID means running.
			if len(fields) >= 1 && fields[0] != "-" {
				return StateRunning, nil
			}
			return StateStopped, nil
		}
	}
	return StateStopped, nil
}

func (m *launchdManager) run(ctx context.Context, name string, args ...string) error {
	_, err := m.runOut(ctx, name, args...)
	return err
}

func (m *launchdManager) runOut(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}
