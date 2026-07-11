package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Brevitas-ai/brevitas/internal/config"
	"github.com/Brevitas-ai/brevitas/internal/provider"
)

func TestClaudeRemovesStaleBrevitasKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	// Simulate what an older Brevitas version wrote: the Brevitas key injected
	// as ANTHROPIC_API_KEY, plus an unrelated user setting.
	orig := `{"env":{"ANTHROPIC_API_KEY":"brevitas-KEY","ANTHROPIC_BASE_URL":"x"},"keep":1}`
	if err := os.WriteFile(settings, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	data := t.TempDir()
	env := &provider.Env{
		Config:   config.Default(),
		Dirs:     config.Dirs{Config: data, Data: data, Logs: data, Cache: data},
		ProxyURL: "http://127.0.0.1:8080",
		APIKey:   func(context.Context) (string, error) { return "brevitas-KEY", nil },
	}
	if err := New(env).Install(context.Background()); err != nil {
		t.Fatalf("install: %v", err)
	}

	raw, _ := os.ReadFile(settings)
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	envMap := root["env"].(map[string]any)
	if _, present := envMap["ANTHROPIC_API_KEY"]; present {
		t.Errorf("stale Brevitas key was not removed: %s", raw)
	}
	if envMap["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:8080" {
		t.Errorf("base url not set: %v", envMap["ANTHROPIC_BASE_URL"])
	}
	if envMap["ANTHROPIC_CUSTOM_HEADERS"] != "X-Brevitas-Client: claude-code" {
		t.Errorf("client label not set: %v", envMap["ANTHROPIC_CUSTOM_HEADERS"])
	}
	if root["keep"] == nil {
		t.Errorf("unrelated setting was dropped: %s", raw)
	}
}

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
