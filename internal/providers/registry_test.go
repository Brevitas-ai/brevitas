package providers

import (
	"context"
	"testing"

	"github.com/brevitas-systems/brevitas/internal/config"
	"github.com/brevitas-systems/brevitas/internal/provider"
)

func testEnv(t *testing.T) *provider.Env {
	t.Helper()
	dir := t.TempDir()
	return &provider.Env{
		Config:   config.Default(),
		Dirs:     config.Dirs{Config: dir, Data: dir, Logs: dir, Cache: dir},
		ProxyURL: "http://127.0.0.1:8080",
		APIKey:   func(context.Context) (string, error) { return "sk-test", nil },
	}
}

func TestRegistryCompleteness(t *testing.T) {
	reg := New(testEnv(t))
	all := reg.All()
	if len(all) != len(factories) {
		t.Fatalf("registry has %d providers, want %d", len(all), len(factories))
	}
	if len(all) < 16 {
		t.Fatalf("expected at least 16 providers, got %d", len(all))
	}
}

func TestProviderNamesUnique(t *testing.T) {
	reg := New(testEnv(t))
	seen := map[string]bool{}
	for _, p := range reg.All() {
		if p.Name() == "" {
			t.Error("empty provider name")
		}
		if seen[p.Name()] {
			t.Errorf("duplicate provider name %q", p.Name())
		}
		seen[p.Name()] = true
	}
}

func TestUnsupportedAndPartialHaveReasons(t *testing.T) {
	ctx := context.Background()
	reg := New(testEnv(t))
	for _, p := range reg.All() {
		st := p.Status(ctx)
		switch p.Support() {
		case provider.SupportUnsupported, provider.SupportPartial:
			if st.Reason == "" {
				t.Errorf("%s (%s) has no reason", p.Name(), p.Support())
			}
		}
	}
}

func TestDetectNeverPanics(t *testing.T) {
	ctx := context.Background()
	reg := New(testEnv(t))
	// Detect and Status must be safe to call on any host.
	for _, p := range reg.All() {
		_ = p.Detect(ctx)
		_ = p.Status(ctx)
	}
	// Parallel detection path.
	_ = reg.Detected(ctx)
}

func TestGetByName(t *testing.T) {
	reg := New(testEnv(t))
	if reg.Get("claude") == nil {
		t.Error("claude not found")
	}
	if reg.Get("does-not-exist") != nil {
		t.Error("unexpected provider")
	}
}
