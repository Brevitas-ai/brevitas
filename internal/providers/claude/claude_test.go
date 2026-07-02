package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Brevitas-ai/brevitas/internal/config"
	"github.com/Brevitas-ai/brevitas/internal/provider"
)

func TestClaudeInstallValidateUninstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // windows

	data := t.TempDir()
	env := &provider.Env{
		Config:   config.Default(),
		Dirs:     config.Dirs{Config: data, Data: data, Logs: data, Cache: data},
		ProxyURL: "http://127.0.0.1:8080",
		APIKey:   func(context.Context) (string, error) { return "sk-brevitas", nil },
	}
	p := New(env)
	ctx := context.Background()

	// Not configured yet.
	if err := p.Validate(ctx); err == nil {
		t.Fatal("expected validation to fail before install")
	}

	if err := p.Install(ctx); err != nil {
		t.Fatalf("install: %v", err)
	}

	settings := filepath.Join(home, ".claude", "settings.json")
	if _, err := os.Stat(settings); err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	if err := p.Validate(ctx); err != nil {
		t.Fatalf("validate after install: %v", err)
	}
	if st := p.Status(ctx); !st.Configured {
		t.Errorf("status not configured: %+v", st)
	}

	if err := p.Uninstall(ctx); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	// Original absent -> settings.json removed on restore.
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Errorf("settings.json should be removed after uninstall, err=%v", err)
	}
}
