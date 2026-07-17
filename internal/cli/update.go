package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Brevitas-ai/brevitas/internal/optimizer"
	"github.com/Brevitas-ai/brevitas/internal/version"
)

const defaultReleaseAPIURL = "https://api.github.com/repos/Brevitas-ai/brevitas/releases/latest"

var releaseHTTPClient = &http.Client{Timeout: 5 * time.Second}

// cmdUpdate checks both the bvx CLI and the separately installed
// brevitas-systems package. CLI upgrades remain package-manager operations;
// brevitas-systems can be upgraded directly by this command.
func (a *App) cmdUpdate(ctx context.Context, args []string) error {
	if helpRequested(args) {
		a.printUpdateHelp()
		return nil
	}
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	assumeYes := fs.Bool("yes", false, "upgrade without prompting")
	fs.BoolVar(assumeYes, "y", false, "shorthand for --yes")
	if err := fs.Parse(args); err != nil {
		return err
	}

	a.page("Updates", "Check BVX and the separately managed optimization engine.")
	a.section("BVX CLI")
	a.checkCLIUpdate(ctx)
	a.section("Optimization engine")

	sys := a.systems()

	current, err := sys.Version(ctx)
	if err != nil {
		a.warn("brevitas-systems is not installed: %v", err)
		if !a.confirm(*assumeYes, "Install brevitas-systems now? [y/N] ") {
			return nil
		}
		return a.doUpgrade(ctx, sys)
	}
	a.metric("Installed", current, ansiCyan)

	pinned := version.PinnedSystemsVersion
	if optimizer.CompareVersions(current, pinned) == 0 {
		a.ok("brevitas-systems is pinned and up to date (%s)", pinned)
		return nil
	}

	a.metric("Pinned", pinned, ansiBlue)
	a.metric("Installed", current, ansiYellow)
	if !a.confirm(*assumeYes, "Install pinned version now? [y/N] ") {
		return nil
	}
	return a.doUpgrade(ctx, sys)
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func (a *App) checkCLIUpdate(ctx context.Context) {
	a.note("Checking GitHub releases…")
	latest, err := latestCLIRelease(ctx)
	if err != nil {
		a.warn("Could not check for a BVX CLI update: %v", err)
		return
	}

	current := strings.TrimPrefix(strings.TrimSpace(version.Version), "v")
	available := strings.TrimPrefix(strings.TrimSpace(latest.TagName), "v")
	if optimizer.CompareVersions(current, available) >= 0 {
		a.ok("BVX CLI is up to date (%s)", current)
		return
	}

	a.warn("BVX CLI update available: %s → %s", current, available)
	a.section("Upgrade command")
	for _, line := range cliUpgradeCommand(runtime.GOOS) {
		a.command(line, "Update BVX")
	}
	if latest.HTMLURL != "" {
		a.note("Release notes: %s", latest.HTMLURL)
	}
}

func latestCLIRelease(ctx context.Context) (githubRelease, error) {
	url := defaultReleaseAPIURL
	if override := os.Getenv("BREVITAS_RELEASE_API_URL"); override != "" {
		url = override
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", version.UserAgent())

	resp, err := releaseHTTPClient.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub returned %s", resp.Status)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	if strings.TrimSpace(release.TagName) == "" {
		return githubRelease{}, fmt.Errorf("GitHub response did not include a release tag")
	}
	return release, nil
}

func cliUpgradeCommand(goos string) []string {
	if goos == "windows" {
		return []string{`irm https://raw.githubusercontent.com/Brevitas-ai/brevitas/main/install.ps1 | iex`}
	}
	return []string{"brew update", "brew upgrade bvx"}
}

func (a *App) doUpgrade(ctx context.Context, sys *optimizer.Systems) error {
	a.note("Upgrading brevitas-systems…")
	if err := sys.Upgrade(ctx); err != nil {
		return err
	}
	v, _ := sys.Version(ctx)
	a.ok("brevitas-systems upgraded to %s", v)
	a.command("bvx restart", "Restart services to load the new engine")
	return nil
}

func (a *App) confirm(assumeYes bool, label string) bool {
	if assumeYes {
		return true
	}
	ans, err := a.prompt(label)
	if err != nil {
		return false
	}
	ans = strings.ToLower(strings.TrimSpace(ans))
	return ans == "y" || ans == "yes"
}
