// Package cli implements the `bvx` command-line interface. It wires the
// installer's components together (config, keyring, provider registry, proxy,
// service manager, optimizer client) and dispatches subcommands.
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/Brevitas-ai/brevitas/internal/config"
	"github.com/Brevitas-ai/brevitas/internal/keyring"
	"github.com/Brevitas-ai/brevitas/internal/logging"
	"github.com/Brevitas-ai/brevitas/internal/optimizer"
	"github.com/Brevitas-ai/brevitas/internal/provider"
	"github.com/Brevitas-ai/brevitas/internal/providers"
)

// App holds the injected dependencies shared by every subcommand.
type App struct {
	Cfg     *config.Config
	Dirs    config.Dirs
	Keyring keyring.Keyring
	Out     io.Writer
	Err     io.Writer
	In      io.Reader

	// dashboardActive prevents commands launched by Home from opening a
	// second dashboard. returnHomeRequested lets full-screen child views jump
	// straight back to the existing Home screen when the user cancels them.
	dashboardActive       bool
	dashboardScreenActive bool
	returnHomeRequested   bool
}

// command is a single CLI subcommand.
type command struct {
	name    string
	summary string
	run     func(a *App, ctx context.Context, args []string) error
}

// commands is the full command table, in display order.
var commands = []command{
	{"install", "Configure AI tools or choose a codebase (`install repo`)", (*App).cmdInstall},
	{"onboard", "Scan a company backend and import existing customers safely", (*App).cmdOnboard},
	{"uninstall", "Restore all tool configs and remove the background service", (*App).cmdUninstall},
	{"status", "Show proxy, service, and provider status", (*App).cmdStatus},
	{"stats", "Show cumulative token-savings metrics from the proxy", (*App).cmdStats},
	{"providers", "List supported providers and their detection/config state", (*App).cmdProviders},
	{"doctor", "Run diagnostics across the whole installation", (*App).cmdDoctor},
	{"repair", "Re-apply configuration and restart the service", (*App).cmdRepair},
	{"start", "Start the background proxy service", (*App).cmdStart},
	{"stop", "Stop the background proxy service", (*App).cmdStop},
	{"restart", "Restart the background proxy service", (*App).cmdRestart},
	{"logs", "Print (or follow) the proxy logs", (*App).cmdLogs},
	{"config", "Print or edit Brevitas configuration", (*App).cmdConfig},
	{"login", "Connect through the Brevitas dashboard and store the key securely", (*App).cmdLogin},
	{"logout", "Remove the stored Brevitas API key", (*App).cmdLogout},
	{"update", "Check for BVX and optimization-engine updates", (*App).cmdUpdate},
	{"serve", "Run the proxy in the foreground (used by the service manager)", (*App).cmdServe},
	{"optimizer", "Run the brevitas-systems optimizer adapter in the foreground", (*App).cmdOptimizer},
	{"version", "Print version information", (*App).cmdVersion},
}

// Main is the process entrypoint. It builds an App and dispatches.
func Main() int {
	logging.Init(logging.Options{
		Level:  logging.ParseLevel(os.Getenv("BREVITAS_LOG_LEVEL")),
		Format: logging.FormatText,
		Output: os.Stderr,
	})

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bvx: failed to load config:", err)
		return 1
	}

	app := &App{
		Cfg:     cfg,
		Dirs:    config.ResolveDirs(),
		Keyring: keyring.Default(),
		Out:     os.Stdout,
		Err:     os.Stderr,
		In:      os.Stdin,
	}

	ctx := context.Background()
	return app.Run(ctx, os.Args[1:])
}

// Run dispatches a single invocation and returns a process exit code.
func (a *App) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return a.runHomeDashboard(ctx)
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		a.usage()
		return 0
	}

	name := args[0]
	for _, c := range commands {
		if c.name == name {
			if isDashboardViewCommand(name) && a.shouldUseInteractiveDashboard() {
				return a.runHomeDashboardWithAction(ctx, args)
			}
			if err := c.run(a, ctx, args[1:]); err != nil {
				if colorEnabled(a.Err) {
					fmt.Fprintf(a.Err, "%s%s✗ bvx %s:%s %v\n", ansiRed, ansiBold, name, ansiReset, err)
				} else {
					fmt.Fprintf(a.Err, "✗ bvx %s: %v\n", name, err)
				}
				return 1
			}
			if a.shouldOpenDashboardAfterCommand(name, args[1:]) {
				return a.runHomeDashboard(ctx)
			}
			return 0
		}
	}

	if colorEnabled(a.Err) {
		fmt.Fprintf(a.Err, "%s%s✗ Unknown command:%s %q\n\n", ansiRed, ansiBold, ansiReset, name)
	} else {
		fmt.Fprintf(a.Err, "✗ Unknown command: %q\n\n", name)
	}
	a.usage()
	return 2
}

func (a *App) runHomeDashboard(ctx context.Context) int {
	return a.runHomeDashboardWithAction(ctx, nil)
}

func (a *App) runHomeDashboardWithAction(ctx context.Context, initialAction []string) int {
	wasActive := a.dashboardActive
	a.dashboardActive = true
	defer func() { a.dashboardActive = wasActive }()

	in, inOK := a.In.(*os.File)
	out, outOK := a.Out.(*os.File)
	if inOK && outOK && canUseArrowNavigator(in, out) {
		wasScreenActive := a.dashboardScreenActive
		a.dashboardScreenActive = true
		enterAlternateScreen(out)
		defer func() {
			leaveAlternateScreen(out)
			a.dashboardScreenActive = wasScreenActive
		}()
	}

	for {
		selected := initialAction
		initialAction = nil
		if len(selected) == 0 {
			var handled bool
			var err error
			selected, handled, err = a.chooseHomeAction()
			if err != nil {
				fmt.Fprintf(a.Err, "bvx: command center: %v\n", err)
				return 1
			}
			if !handled {
				a.home()
				return 0
			}
			if len(selected) == 0 {
				return 0
			}
		}

		renderHomeActionScreen(a.Out)
		_ = a.Run(ctx, selected)
		if a.returnHomeRequested {
			a.returnHomeRequested = false
			continue
		}
		if isHomeCommandReference(selected) {
			commandArgs, quit, promptErr := a.promptHomeCommand()
			if promptErr != nil {
				fmt.Fprintf(a.Err, "bvx: command prompt: %v\n", promptErr)
				return 1
			}
			if quit {
				return 0
			}
			if len(commandArgs) == 0 {
				continue
			}
			renderHomeActionScreen(a.Out)
			_ = a.Run(ctx, commandArgs)
		}
		back, waitErr := a.waitForHome()
		if waitErr != nil {
			fmt.Fprintf(a.Err, "bvx: return home: %v\n", waitErr)
			return 1
		}
		if !back {
			return 0
		}
	}
}

func isDashboardViewCommand(name string) bool {
	return name == "stats"
}

func (a *App) shouldUseInteractiveDashboard() bool {
	if a.dashboardActive || os.Getenv("CI") != "" {
		return false
	}
	in, inOK := a.In.(*os.File)
	out, outOK := a.Out.(*os.File)
	return inOK && outOK && canUseArrowNavigator(in, out)
}

// shouldOpenDashboardAfterCommand keeps account setup cohesive: interactive
// installs and logins finish at Home. Piped commands, help output, CI, and
// actions already launched from Home stay non-interactive.
func (a *App) shouldOpenDashboardAfterCommand(name string, args []string) bool {
	return isDashboardLandingCommand(name) && !helpRequested(args) && a.shouldUseInteractiveDashboard()
}

func isDashboardLandingCommand(name string) bool {
	return name == "install" || name == "login"
}

func isHomeCommandReference(args []string) bool {
	return len(args) == 1 && args[0] == "help"
}

func (a *App) usage() {
	a.page("Command reference", "Optimize AI work without changing how you work.")
	fmt.Fprintf(a.Out, "\n  %s  %s\n", a.styled(ansiPink+ansiBold, "USAGE"), a.styled(ansiCyan+ansiBold, "bvx <command> [flags]"))
	a.section("Commands")
	rows := make([]command, len(commands))
	copy(rows, commands)
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	for _, c := range rows {
		a.command("bvx "+c.name, c.summary)
	}
	fmt.Fprintln(a.Out)
	a.note("Run `bvx <command> --help` for command-specific options.")
	fmt.Fprintln(a.Out)
}

// --- shared helpers -------------------------------------------------------

// registry builds the provider registry bound to an injected Env.
func (a *App) registry() *providers.Registry {
	return providers.New(a.env())
}

// env constructs the provider Env with the keyring-backed API key source.
func (a *App) env() *provider.Env {
	return &provider.Env{
		Config:   a.Cfg,
		Dirs:     a.Dirs,
		Logger:   logging.L(),
		ProxyURL: a.Cfg.ProxyURL(),
		APIKey:   a.apiKeyFunc(),
	}
}

// apiKeyFunc returns a function that reads the Brevitas API key from the OS
// credential store (honoring the BREVITAS_API_KEY override for CI/testing).
func (a *App) apiKeyFunc() provider.APIKeyFunc {
	return func(ctx context.Context) (string, error) {
		if v := os.Getenv("BREVITAS_API_KEY"); v != "" {
			return v, nil
		}
		return a.Keyring.Get(ctx)
	}
}

// optimizerClient builds the brevitas-systems client from config.
func (a *App) optimizerClient() optimizer.Client {
	return optimizer.New(a.Cfg.Optimizer)
}

// systems builds the brevitas-systems probe.
func (a *App) systems() *optimizer.Systems {
	return optimizer.NewSystems(a.Cfg.Optimizer.PythonBin)
}

// hasKey reports whether an API key is stored.
func (a *App) hasKey(ctx context.Context) bool {
	k, err := a.apiKeyFunc()(ctx)
	return err == nil && k != ""
}

// prompt reads a single line of visible input.
func (a *App) prompt(label string) (string, error) {
	fmt.Fprint(a.Out, label)
	r := bufio.NewReader(a.In)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// promptSecret reads a secret without echoing it to the terminal when possible.
func (a *App) promptSecret(label string) (string, error) {
	fmt.Fprint(a.Out, label)

	restore := disableEcho(a.In)
	defer restore()

	r := bufio.NewReader(a.In)
	line, err := r.ReadString('\n')
	fmt.Fprintln(a.Out)
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// disableEcho best-effort turns off terminal echo on Unix via stty and returns
// a restore function. On Windows (or when stdin is not a TTY) it is a no-op.
func disableEcho(in io.Reader) func() {
	if in != os.Stdin || runtime.GOOS == "windows" {
		return func() {}
	}
	if fi, err := os.Stdin.Stat(); err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		return func() {} // piped input: nothing to hide
	}
	cmd := exec.Command("stty", "-echo")
	cmd.Stdin = os.Stdin
	if cmd.Run() != nil {
		return func() {}
	}
	return func() {
		c := exec.Command("stty", "echo")
		c.Stdin = os.Stdin
		_ = c.Run()
	}
}

// ok/warn/fail print consistent status glyphs.
func (a *App) ok(format string, args ...any) {
	fmt.Fprintf(a.Out, "  %s %s\n", a.styled(ansiGreen+ansiBold, "✓"), fmt.Sprintf(format, args...))
}
func (a *App) warn(format string, args ...any) {
	fmt.Fprintf(a.Out, "  %s %s\n", a.styled(ansiYellow+ansiBold, "⚠"), fmt.Sprintf(format, args...))
}
func (a *App) fail(format string, args ...any) {
	fmt.Fprintf(a.Out, "  %s %s\n", a.styled(ansiRed+ansiBold, "✗"), fmt.Sprintf(format, args...))
}
func (a *App) say(format string, args ...any) { fmt.Fprintf(a.Out, format+"\n", args...) }
