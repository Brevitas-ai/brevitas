package cli

import "fmt"

type helpOption struct {
	flag        string
	description string
}

func helpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func (a *App) commandHelp(title, subtitle, usage string, options []helpOption) {
	a.page(title, subtitle)
	fmt.Fprintf(a.Out, "\n  %s  %s\n", a.styled(ansiPink+ansiBold, "USAGE"), a.styled(ansiCyan+ansiBold, usage))
	if len(options) == 0 {
		return
	}
	a.section("Options")
	for _, option := range options {
		a.command(option.flag, option.description)
	}
}

func (a *App) printLoginHelp() {
	a.commandHelp("Connect your account", "Authorize BVX and securely store a revocable device key.", "bvx login [flags]", []helpOption{
		{"--api-key KEY", "Use a key directly for CI and automation"},
		{"--no-open", "Print the authorization URL without opening a browser"},
		{"-h, --help", "Show this help"},
	})
}

func (a *App) printInstallAIHelp() {
	a.commandHelp("Install AI tools", "Detect and configure supported local AI clients.", "bvx install ai [flags]", []helpOption{
		{"--api-key KEY", "Use a key directly for CI and automation"},
		{"--no-service", "Configure tools without installing background services"},
		{"-h, --help", "Show this help"},
	})
}

func (a *App) printCodebaseHelp(repo string) {
	a.commandHelp("Connect a repository", "Scan and route a known codebase path.", "bvx install "+repo+" [flags]", []helpOption{
		{"--apply", "Write routing variables to .env.agentmap"},
		{"--auto", "Also rewrite hardcoded provider URLs"},
		{"--api-key KEY", "Use a key directly for CI and automation"},
		{"--no-open", "Do not open the HTML scan report"},
		{"--target URL", "Override the gateway URL"},
		{"--environment NAME", "Inventory this deployment environment (default: local)"},
		{"-h, --help", "Show this help"},
	})
}

func (a *App) printOnboardHelp() {
	a.commandHelp("Onboard a company backend", "Scan AI traffic and import exact customer identities.", "bvx onboard [guide|demo] [flags] [repo]", []helpOption{
		{"--customers FILE", "Past-customer export: CSV, TSV, JSON, JSONL, or NDJSON"},
		{"--id-field PATH", "Explicit stable ID field, such as customer.uuid"},
		{"--name-field PATH", "Opt in to a display-name field, such as profile.company"},
		{"--apply", "Apply codebase routing and import validated customers"},
		{"--auto", "With --apply, also rewrite hardcoded provider URLs"},
		{"--skip-invalid", "Import valid records while reporting rejected rows"},
		{"--skip-scan", "Import customer data without scanning a codebase"},
		{"--api-key KEY", "Use a key directly for CI; otherwise browser login"},
		{"--no-open", "Do not open the AgentMap report"},
		{"--target URL", "Override the Brevitas gateway URL"},
		{"--environment NAME", "Inventory this deployment environment"},
		{"--guide", "Open the step-by-step onboarding guide"},
		{"--demo", "Open the Brevitas dashboard demo"},
		{"-h, --help", "Show this help"},
	})
}

func (a *App) printProvidersHelp() {
	a.commandHelp("AI tool compatibility", "Inspect detection and configuration support.", "bvx providers [flags]", []helpOption{
		{"--detected", "Show only tools detected on this machine"},
		{"-h, --help", "Show this help"},
	})
}

func (a *App) printLogsHelp() {
	a.commandHelp("Proxy logs", "Read or follow the local BVX proxy log.", "bvx logs [flags]", []helpOption{
		{"-f, --follow", "Continue following new log entries"},
		{"-h, --help", "Show this help"},
	})
}

func (a *App) printUninstallHelp() {
	a.commandHelp("Uninstall", "Restore tool configuration and remove BVX services.", "bvx uninstall [flags]", []helpOption{
		{"--purge", "Also remove the stored Brevitas API key"},
		{"-h, --help", "Show this help"},
	})
}

func (a *App) printUpdateHelp() {
	a.commandHelp("Updates", "Check BVX and optimization-engine versions.", "bvx update [flags]", []helpOption{
		{"-y, --yes", "Upgrade without prompting"},
		{"-h, --help", "Show this help"},
	})
}
