package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	c := Default()
	if c.Proxy.Port != DefaultProxyPort {
		t.Fatalf("port = %d, want %d", c.Proxy.Port, DefaultProxyPort)
	}
	if c.ProxyURL() != "http://127.0.0.1:8080" {
		t.Fatalf("proxy url = %q", c.ProxyURL())
	}
	for _, fam := range []string{"openai", "anthropic", "google"} {
		if c.Upstreams[fam] == "" {
			t.Errorf("missing upstream for %s", fam)
		}
	}
}

func TestDeviceIDIsRandomStableAndPseudonymous(t *testing.T) {
	first := Default()
	id, err := first.EnsureDeviceID()
	if err != nil {
		t.Fatal(err)
	}
	again, err := first.EnsureDeviceID()
	if err != nil || again != id {
		t.Fatalf("device id changed: %q -> %q (%v)", id, again, err)
	}
	second := Default()
	other, err := second.EnsureDeviceID()
	if err != nil || other == id {
		t.Fatalf("device ids are not independently random: %q %q (%v)", id, other, err)
	}
	if !strings.HasPrefix(id, "dev_") || len(id) != len("dev_")+32 {
		t.Fatalf("unexpected device id shape: %q", id)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	c := Default()
	c.Proxy.Port = 9090
	c.AddProvider("claude")
	if err := c.SaveTo(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Proxy.Port != 9090 {
		t.Errorf("port = %d, want 9090", got.Proxy.Port)
	}
	if len(got.EnabledProviders) != 1 || got.EnabledProviders[0] != "claude" {
		t.Errorf("providers = %v", got.EnabledProviders)
	}
}

func TestAddRemoveProviderIdempotent(t *testing.T) {
	c := Default()
	c.AddProvider("claude")
	c.AddProvider("claude")
	if len(c.EnabledProviders) != 1 {
		t.Fatalf("expected 1, got %v", c.EnabledProviders)
	}
	c.AddProvider("cursor")
	c.RemoveProvider("claude")
	if len(c.EnabledProviders) != 1 || c.EnabledProviders[0] != "cursor" {
		t.Fatalf("after remove: %v", c.EnabledProviders)
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	got, err := LoadFrom(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("expected default, got err %v", err)
	}
	if got.Proxy.Port != DefaultProxyPort {
		t.Fatalf("expected default port")
	}
}
