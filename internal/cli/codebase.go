package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Brevitas-ai/brevitas/internal/codebase"
	"github.com/Brevitas-ai/brevitas/internal/optimizer"
)

// installCodebase scans a repository and wires Brevitas into its agent code.
//
// The dedicated internal scanner is not built yet (see internal/codebase). Until
// it ships as a pip package, this command explains the flow and — if the
// brevitas-systems package is installed — runs `brevitas analyze` as an interim
// preview of the call sites Brevitas will optimize.
func (a *App) installCodebase(ctx context.Context, repo string, args []string) error {
	fs := flag.NewFlagSet("install <repo>", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	apply := fs.Bool("apply", false, "wire Brevitas into detected call sites (default: report only)")
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

	a.say("Scanning codebase: %s\n", abs)

	// Try the internal scanner first (not available yet).
	res, err := codebase.New().Scan(ctx, abs, codebase.Options{
		Apply:     *apply,
		PythonBin: a.Cfg.Optimizer.PythonBin,
	})
	switch {
	case err == nil:
		return a.reportCodebase(res)
	case !errors.Is(err, codebase.ErrNotAvailable):
		return err
	}

	// Scanner not built yet — explain what it will do.
	a.warn("The Brevitas codebase scanner is coming soon.")
	a.say("When available, `bvx install %s` will:", repo)
	a.say("  1. Find every LLM API call site (OpenAI/Anthropic/Google SDK + raw HTTP) and its key")
	a.say("  2. Recommend optimize vs lossless per call")
	a.say("  3. Wire the Brevitas model between your agents and the provider to cut tokens")

	// Interim preview using the existing brevitas-systems analyzer.
	if cli := a.brevitasCLI(ctx); cli != "" {
		a.say("\nInterim preview (brevitas analyze):\n")
		verb := "analyze"
		if *apply {
			verb = "apply" // brevitas apply wraps clients; add --write yourself once reviewed
		}
		return runForeground(ctx, cli, []string{verb, abs}, a.Out)
	}
	a.say("\nInterim: `pip install brevitas-systems`, then `brevitas analyze %s`.", abs)
	return nil
}

// reportCodebase prints a scan result (used once the internal scanner ships).
func (a *App) reportCodebase(res *codebase.Result) error {
	a.say("Found %d LLM call site(s) in %s", len(res.CallSites), res.Repo)
	for _, cs := range res.CallSites {
		a.say("  %s:%d  %s  %s", cs.File, cs.Line, cs.Provider, cs.Strategy)
	}
	if len(res.Wrote) > 0 {
		a.say("\nWired Brevitas into %d file(s).", len(res.Wrote))
	}
	return nil
}

// brevitasCLI returns the path to the brevitas-systems console script that sits
// next to the interpreter which can import it, or "" if unavailable.
func (a *App) brevitasCLI(ctx context.Context) string {
	py := optimizer.DetectPython(ctx, a.Cfg.Optimizer.PythonBin)
	if py == "" {
		return ""
	}
	cli := filepath.Join(filepath.Dir(py), "brevitas")
	if _, err := os.Stat(cli); err == nil {
		return cli
	}
	return ""
}
