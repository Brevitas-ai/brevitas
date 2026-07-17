package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Brevitas-ai/brevitas/internal/cloud"
	"github.com/Brevitas-ai/brevitas/internal/optimizer"
)

// installCodebase scans a repository with the agentmap-scan package and,
// optionally, routes its LLM calls through the Brevitas proxy so the optimizer
// reduces tokens on every provider call.
//
//	bvx install <repo>                 scan + open the AI-call map
//	bvx install <repo> --apply         also route the codebase through Brevitas
//	bvx install <repo> --apply --auto  also rewrite hardcoded provider URLs
func (a *App) installCodebase(ctx context.Context, repo string, args []string) error {
	if helpRequested(args) {
		a.printCodebaseHelp(repo)
		return nil
	}
	fs := flag.NewFlagSet("install <repo>", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	apply := fs.Bool("apply", false, "route the codebase's LLM calls through Brevitas (writes .env.agentmap)")
	auto := fs.Bool("auto", false, "with --apply, also rewrite hardcoded provider URLs in place")
	apiKeyFlag := fs.String("api-key", "", "Brevitas API key (for CI; otherwise browser login)")
	noOpen := fs.Bool("no-open", false, "do not open the HTML report in a browser")
	target := fs.String("target", a.Cfg.ProxyURL(), "gateway URL to route calls through")
	if err := fs.Parse(args); err != nil {
		return err
	}

	abs, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	if info, statErr := os.Stat(abs); statErr != nil || !info.IsDir() {
		return fmt.Errorf("%q is not a directory (use `bvx install ai` for AI tools, or pass a repo path)", repo)
	}

	cli := a.agentmapCLI(ctx)
	if cli == "" {
		a.page("Connect a repository", "Scan and route a codebase through Brevitas.")
		a.warn("The Brevitas codebase scanner is not installed.")
		a.section("Install the scanner")
		a.command("pip install agentmap-scan", "Install AgentMap")
		a.command("bvx install "+repo, "Retry this repository")
		return nil
	}
	a.page("Connect a repository", "Authorize, scan, and register this codebase safely.")
	if err := a.ensureAPIKey(ctx, *apiKeyFlag); err != nil {
		return err
	}

	// 1. Scan: map every AI call in the codebase locally. The Brevitas account
	// key is used only to register the safe repository label in the dashboard.
	a.section("Scanning repository")
	a.note("%s", abs)
	scanArgs := []string{"scan", abs, "--target", *target}
	if *noOpen {
		scanArgs = append(scanArgs, "--no-open")
	}
	if err := runForeground(ctx, cli, scanArgs, a.Out); err != nil {
		return fmt.Errorf("agentmap scan: %w", err)
	}
	if key, keyErr := a.apiKeyFunc()(ctx); keyErr != nil {
		a.warn("Could not register this repository in the dashboard: %v", keyErr)
	} else if registerErr := cloud.RegisterRepository(ctx, key, filepath.Base(abs)); registerErr != nil {
		a.warn("Repository scanned, but dashboard registration is unavailable: %v", registerErr)
	} else {
		a.ok("Repository connected to your Brevitas dashboard")
	}

	if !*apply {
		a.section("Next step")
		a.command("bvx install "+repo+" --apply", "Route this repository through Brevitas")
		return nil
	}
	// 2. Apply: write routing env vars (and optionally rewrite hardcoded URLs)
	//    so the codebase's calls flow through the Brevitas proxy.
	a.section("Applying routing")
	a.note("%s → %s", repo, *target)
	installArgs := []string{"install", abs, "--target", *target,
		"--env-file", filepath.Join(abs, ".env.agentmap")}
	if *auto {
		installArgs = append(installArgs, "--auto")
	}
	if err := runForeground(ctx, cli, installArgs, a.Out); err != nil {
		return fmt.Errorf("agentmap install: %w", err)
	}
	a.success("Repository installation complete")
	a.command("source "+abs+"/.env.agentmap", "Load routing variables before running agents")
	a.installServices(ctx)
	a.command("bvx status", "Check the installation at any time")
	return nil
}

// agentmapCLI returns the path to the agentmap console script, or "" if the
// agentmap-scan package is not installed.
func (a *App) agentmapCLI(ctx context.Context) string {
	if p, err := exec.LookPath("agentmap"); err == nil {
		return p
	}
	// agentmap-scan is typically installed alongside brevitas-systems; look for
	// its console script next to the interpreter that can import brevitas.
	if py := optimizer.DetectPython(ctx, a.Cfg.Optimizer.PythonBin); py != "" {
		cli := filepath.Join(filepath.Dir(py), "agentmap")
		if _, err := os.Stat(cli); err == nil {
			return cli
		}
	}
	return ""
}
