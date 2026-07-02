package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brevitas-systems/brevitas/internal/config"
)

func testBase(t *testing.T, name string) (*Base, config.Dirs) {
	t.Helper()
	dir := t.TempDir()
	dirs := config.Dirs{Config: dir, Data: dir, Logs: dir, Cache: dir}
	env := &Env{
		Dirs:     dirs,
		ProxyURL: "http://127.0.0.1:8080",
	}
	b := NewBase(env, name, name, SupportFull, "")
	return &b, dirs
}

func TestEditJSONCreatesAndMerges(t *testing.T) {
	b, _ := testBase(t, "claude")
	file := filepath.Join(t.TempDir(), "settings.json")

	// Seed an existing, unrelated key to prove we don't destroy it.
	if err := os.WriteFile(file, []byte(`{"keepMe":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := b.EditJSON(file, func(root map[string]any) error {
		env, _ := root["env"].(map[string]any)
		if env == nil {
			env = map[string]any{}
		}
		env["ANTHROPIC_BASE_URL"] = b.ProxyURL()
		root["env"] = env
		return nil
	})
	if err != nil {
		t.Fatalf("EditJSON: %v", err)
	}

	data, _ := os.ReadFile(file)
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	if root["keepMe"] != true {
		t.Error("existing key was destroyed")
	}
	if got := root["env"].(map[string]any)["ANTHROPIC_BASE_URL"]; got != b.ProxyURL() {
		t.Errorf("base url = %v", got)
	}
}

func TestRestoreExistingFile(t *testing.T) {
	b, _ := testBase(t, "claude")
	file := filepath.Join(t.TempDir(), "settings.json")
	original := `{"original":true}`
	if err := os.WriteFile(file, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := b.EditJSON(file, func(root map[string]any) error {
		root["added"] = "x"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}

	got, _ := os.ReadFile(file)
	if string(got) != original {
		t.Errorf("restore mismatch:\n got %s\nwant %s", got, original)
	}
}

func TestRestoreAbsentFileIsRemoved(t *testing.T) {
	b, _ := testBase(t, "aider")
	file := filepath.Join(t.TempDir(), "created.yml")

	if err := b.EditManagedBlock(file, "openai-api-base: \"http://127.0.0.1:8080/v1\""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("file should exist after edit: %v", err)
	}
	if err := b.Restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("file should be removed on restore, err=%v", err)
	}
}

func TestManagedBlockReplaceIsStable(t *testing.T) {
	b, _ := testBase(t, "goose")
	file := filepath.Join(t.TempDir(), "config.yaml")

	if err := os.WriteFile(file, []byte("user_key: preserved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := b.EditManagedBlock(file, "OPENAI_HOST: \"http://127.0.0.1:8080\""); err != nil {
		t.Fatal(err)
	}
	// Second edit must replace, not duplicate, the managed block.
	if err := b.EditManagedBlock(file, "OPENAI_HOST: \"http://127.0.0.1:9090\""); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(file)
	s := string(data)
	if !strings.Contains(s, "user_key: preserved") {
		t.Error("user content lost")
	}
	if strings.Count(s, blockStart) != 1 {
		t.Errorf("expected exactly one managed block, got %d", strings.Count(s, blockStart))
	}
	if !strings.Contains(s, "9090") || strings.Contains(s, "8080") {
		t.Error("managed block not replaced")
	}
}
