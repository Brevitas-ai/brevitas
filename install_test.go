package main

import (
	"os"
	"strings"
	"testing"
)

func TestWindowsInstallerAvoidsAnonymousGitHubAPI(t *testing.T) {
	script, err := os.ReadFile("install.ps1")
	if err != nil {
		t.Fatalf("read install.ps1: %v", err)
	}

	source := string(script)
	if strings.Contains(source, "api.github.com") {
		t.Fatal("install.ps1 must not use GitHub's anonymously rate-limited REST API")
	}
	if !strings.Contains(source, "https://github.com/$Repo/releases/latest") {
		t.Fatal("install.ps1 must resolve the latest release through GitHub's web redirect")
	}
	if !strings.Contains(source, "$request.Method = 'HEAD'") {
		t.Fatal("install.ps1 should resolve the redirect without downloading the releases page")
	}
	if !strings.Contains(source, "$releaseUri.Host -ne 'github.com'") {
		t.Fatal("install.ps1 must validate the release redirect host")
	}
}
