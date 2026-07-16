package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Brevitas-ai/brevitas/internal/version"
)

func TestCheckCLIUpdateAlertsWhenReleaseIsNewer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("User-Agent"), "brevitas/") {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v0.1.22","html_url":"https://example.test/releases/v0.1.22"}`)
	}))
	defer server.Close()
	t.Setenv("BREVITAS_RELEASE_API_URL", server.URL)
	setTestCLIVersion(t, "0.1.21")

	var output bytes.Buffer
	app := &App{Out: &output, Err: &output}
	app.checkCLIUpdate(context.Background())

	for _, want := range []string{
		"BVX CLI update available: 0.1.21 → 0.1.22",
		"brew update",
		"brew upgrade bvx",
		"https://example.test/releases/v0.1.22",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("missing %q in output:\n%s", want, output.String())
		}
	}
}

func TestCheckCLIUpdateReportsCurrentVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v0.1.21"}`)
	}))
	defer server.Close()
	t.Setenv("BREVITAS_RELEASE_API_URL", server.URL)
	setTestCLIVersion(t, "0.1.21")

	var output bytes.Buffer
	app := &App{Out: &output, Err: &output}
	app.checkCLIUpdate(context.Background())
	if !strings.Contains(output.String(), "BVX CLI is up to date (0.1.21)") {
		t.Fatalf("output:\n%s", output.String())
	}
}

func TestCheckCLIUpdateFailureIsOnlyAWarning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	t.Setenv("BREVITAS_RELEASE_API_URL", server.URL)

	var output bytes.Buffer
	app := &App{Out: &output, Err: &output}
	app.checkCLIUpdate(context.Background())
	if !strings.Contains(output.String(), "Could not check for a BVX CLI update") {
		t.Fatalf("output:\n%s", output.String())
	}
}

func TestCLIUpgradeCommandByPlatform(t *testing.T) {
	if got := strings.Join(cliUpgradeCommand("darwin"), "\n"); got != "brew update\nbrew upgrade bvx" {
		t.Fatalf("macOS command = %q", got)
	}
	if got := strings.Join(cliUpgradeCommand("linux"), "\n"); got != "brew update\nbrew upgrade bvx" {
		t.Fatalf("Linux command = %q", got)
	}
	if got := strings.Join(cliUpgradeCommand("windows"), "\n"); !strings.HasPrefix(got, "irm ") {
		t.Fatalf("Windows command = %q", got)
	}
}

func setTestCLIVersion(t *testing.T, value string) {
	t.Helper()
	previous := version.Version
	version.Version = value
	t.Cleanup(func() { version.Version = previous })
}
