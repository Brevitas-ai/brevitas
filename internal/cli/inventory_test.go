package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Brevitas-ai/brevitas/internal/cloud"
	"github.com/Brevitas-ai/brevitas/internal/config"
	"github.com/Brevitas-ai/brevitas/internal/keyring"
)

func TestSafeInventoryLabelNeverReturnsPath(t *testing.T) {
	for input, want := range map[string]string{
		"/Users/alice/private/finance-api":     "finance-api",
		`C:\\Users\\alice\\secret\\ledger.git`: "ledger",
		"/srv/app/bad\nname":                   "badname",
	} {
		if got := safeInventoryLabel(input, 128); got != want {
			t.Errorf("safeInventoryLabel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInstallationRegistrationAndHeartbeatContainOnlySafeMetadata(t *testing.T) {
	var registrationBody, heartbeatBody []byte
	var authHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		authHeaders = append(authHeaders, r.Header.Get("X-Brevitas-Key"))
		switch {
		case r.URL.Path == "/v1/installations":
			registrationBody = body
			var in cloud.InstallationRegistration
			_ = json.Unmarshal(body, &in)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"installation_id":            in.InstallationID,
				"heartbeat_interval_seconds": 60,
			})
		case strings.HasSuffix(r.URL.Path, "/heartbeat"):
			heartbeatBody = body
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "active", "heartbeat_interval_seconds": 120,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("BREVITAS_API_URL", server.URL)

	home := t.TempDir()
	t.Setenv("BREVITAS_HOME", home)
	repo := filepath.Join(home, "private", "finance-backend")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	keys := keyring.NewMemory()
	if err := keys.Set(context.Background(), "bvt_org_secret"); err != nil {
		t.Fatal(err)
	}
	app := &App{Cfg: config.Default(), Dirs: config.ResolveDirs(), Keyring: keys}
	if err := app.registerCodebaseInstallation(context.Background(), "bvt_org_secret", repo, "production"); err != nil {
		t.Fatal(err)
	}
	if len(app.Cfg.Inventory.Installations) != 1 {
		t.Fatalf("installations = %#v", app.Cfg.Inventory.Installations)
	}
	item := &app.Cfg.Inventory.Installations[0]
	item.LastHeartbeatAt = time.Now().Add(-time.Hour)
	app.inventoryCycle(context.Background(), time.Now(), slog.New(slog.NewTextHandler(io.Discard, nil)))

	combined := append(append([]byte{}, registrationBody...), heartbeatBody...)
	for _, forbidden := range []string{home, repo, "bvt_org_secret", "provider", "prompt", "remote", "customer_id"} {
		if bytes.Contains(combined, []byte(forbidden)) {
			t.Fatalf("inventory payload leaked %q: %s", forbidden, combined)
		}
	}
	for _, required := range []string{`"finance-backend"`, `"production"`, `"device"`, `"client"`} {
		if !bytes.Contains(combined, []byte(required)) {
			t.Fatalf("inventory payload missing %s: %s", required, combined)
		}
	}
	if len(heartbeatBody) == 0 || len(authHeaders) != 2 || authHeaders[0] != "bvt_org_secret" || authHeaders[1] != "bvt_org_secret" {
		t.Fatalf("heartbeat/auth not sent: bodies=%q auth=%v", heartbeatBody, authHeaders)
	}
	configBytes, err := os.ReadFile(app.Dirs.ConfigFile())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(configBytes, []byte("bvt_org_secret")) {
		t.Fatal("organization key was persisted outside the native keyring")
	}
}

func TestInventoryIdentifiersAreStableAndEnvironmentScoped(t *testing.T) {
	device := "dev_0123456789abcdef"
	repoA := inventoryID("repo_", device, "finance-backend")
	repoB := inventoryID("repo_", device, "finance-backend")
	if repoA != repoB || !strings.HasPrefix(repoA, "repo_") {
		t.Fatalf("repository ids are not stable: %q %q", repoA, repoB)
	}
	production := installationUUID(device, repoA, "production")
	staging := installationUUID(device, repoA, "staging")
	if production == staging {
		t.Fatal("production and staging installations must not share identity")
	}
	if len(production) != 36 || strings.HasPrefix(production, "ins_") {
		t.Fatalf("installation id is not backend-compatible UUID: %q", production)
	}
}

func TestInventorySaveDoesNotClobberConcurrentConfigSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BREVITAS_HOME", home)
	path := config.ResolveDirs().ConfigFile()
	latest := config.Default()
	latest.Proxy.Port = 9191
	if err := latest.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	stale := config.Default()
	stale.Proxy.Port = 8080
	stale.Inventory.DeviceID = "dev_stable"
	app := &App{Cfg: stale, Dirs: config.ResolveDirs(), Keyring: keyring.NewMemory()}
	if err := app.saveInventoryConfig(); err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Proxy.Port != 9191 || got.Inventory.DeviceID != "dev_stable" {
		t.Fatalf("merged config = %#v", got)
	}
}
