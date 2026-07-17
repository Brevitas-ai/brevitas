package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Brevitas-ai/brevitas/internal/config"
	"github.com/Brevitas-ai/brevitas/internal/provider"
)

func TestInstallUsesBuiltInAuthenticationAndMigratesOldBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(codexDir, "config.toml")
	old := `# >>> brevitas (managed) >>>
model_provider = "brevitas"

[model_providers.brevitas]
base_url = "http://127.0.0.1:8080/v1"
env_key = "OPENAI_API_KEY"
# <<< brevitas (managed) <<<

model = "gpt-5"
`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	data := t.TempDir()
	env := &provider.Env{
		Config:   config.Default(),
		Dirs:     config.Dirs{Config: data, Data: data, Logs: data, Cache: data},
		ProxyURL: "http://127.0.0.1:9090",
	}
	p := New(env)
	if err := p.Install(context.Background()); err != nil {
		t.Fatalf("install: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if !strings.Contains(got, `openai_base_url = "http://127.0.0.1:9090/v1"`) {
		t.Fatalf("built-in provider proxy URL missing: %s", got)
	}
	for _, forbidden := range []string{"OPENAI_API_KEY", "model_provider =", "[model_providers.brevitas]"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("stale custom-provider setting %q remains: %s", forbidden, got)
		}
	}
	if !strings.Contains(got, `model = "gpt-5"`) {
		t.Errorf("unrelated Codex setting was lost: %s", got)
	}
	if err := p.Validate(context.Background()); err != nil {
		t.Fatalf("validate: %v", err)
	}
}
