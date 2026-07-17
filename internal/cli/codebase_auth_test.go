package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Brevitas-ai/brevitas/internal/config"
	"github.com/Brevitas-ai/brevitas/internal/keyring"
)

func TestInstallCodebaseAuthenticatesScansAndRegistersRepository(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test scanner uses a POSIX shell script")
	}

	var registered struct {
		Repo   string `json:"repo"`
		Source string `json:"source"`
	}
	var registeredKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/device-auth/start":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":               "demo_device_code",
				"verification_uri_complete": "https://example.test/dashboard#bvx=demo_device_code",
				"expires_in":                60,
				"interval":                  0,
			})
		case "/v1/device-auth/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"api_key": "bvt_repo_install"})
		case "/v1/repositories":
			registeredKey = r.Header.Get("X-Brevitas-Key")
			_ = json.NewDecoder(r.Body).Decode(&registered)
			_ = json.NewEncoder(w).Encode(map[string]bool{"registered": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("BREVITAS_API_URL", server.URL)

	temp := t.TempDir()
	repo := filepath.Join(temp, "checkout-service")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(temp, "agentmap-args")
	scanner := filepath.Join(temp, "agentmap")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$BVX_TEST_AGENTMAP_ARGS\"\n"
	if err := os.WriteFile(scanner, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", temp+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BVX_TEST_AGENTMAP_ARGS", argsFile)
	t.Setenv("BREVITAS_HOME", filepath.Join(temp, "brevitas-home"))

	opened := ""
	previousOpenBrowser := openBrowser
	openBrowser = func(url string) error { opened = url; return nil }
	defer func() { openBrowser = previousOpenBrowser }()

	keys := keyring.NewMemory()
	var output bytes.Buffer
	app := &App{
		Cfg: config.Default(), Dirs: config.ResolveDirs(), Keyring: keys,
		In: strings.NewReader(""), Out: &output, Err: &output,
	}
	if err := app.installCodebase(context.Background(), repo, []string{"--no-open"}); err != nil {
		t.Fatal(err)
	}

	stored, err := keys.Get(context.Background())
	if err != nil || stored != "bvt_repo_install" {
		t.Fatalf("stored key = %q, %v", stored, err)
	}
	if opened != "https://example.test/dashboard#bvx=demo_device_code" {
		t.Fatalf("opened dashboard URL = %q", opened)
	}
	if registeredKey != "bvt_repo_install" {
		t.Fatalf("registered with key %q", registeredKey)
	}
	if registered.Repo != "checkout-service" || registered.Source != "bvx" {
		t.Fatalf("repository registration = %#v", registered)
	}

	scanArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"scan", repo, "--target", app.Cfg.ProxyURL(), "--no-open"} {
		if !strings.Contains(string(scanArgs), expected) {
			t.Fatalf("scanner args missing %q: %q", expected, scanArgs)
		}
	}
	for _, expected := range []string{
		"BROWSER AUTHORIZATION", "API key stored", "SCANNING REPOSITORY",
		"Repository connected to your Brevitas dashboard",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("CLI output missing %q:\n%s", expected, output.String())
		}
	}
	t.Logf("safe end-to-end CLI transcript:\n%s", output.String())
}
