// Package claude integrates Claude Code (Anthropic's CLI) with Brevitas by
// setting ANTHROPIC_BASE_URL in its documented settings.json env block.
package claude

import (
	"context"
	"path/filepath"

	"github.com/Brevitas-ai/brevitas/internal/detect"
	"github.com/Brevitas-ai/brevitas/internal/provider"
)

// Provider integrates Claude Code.
type Provider struct{ provider.Base }

// New constructs the Claude Code provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "claude", "Claude Code", provider.SupportFull, "")}
}

func (p *Provider) settingsPath() string {
	return filepath.Join(detect.HomeDir(), ".claude", "settings.json")
}

// Detect looks for the Claude Code CLI or its config directory.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.Executable("claude") ||
		detect.Exists(filepath.Join(detect.HomeDir(), ".claude")) ||
		detect.EnvSet("ANTHROPIC_API_KEY")
}

// Install points Claude Code at the Brevitas proxy via settings.json. It sets
// only the base URL — Claude Code keeps using the user's own Anthropic auth
// (subscription token or API key), which the proxy forwards through unchanged.
//
// It also removes an ANTHROPIC_API_KEY that a previous Brevitas version
// injected (equal to the stored Brevitas key), which otherwise persists across
// upgrades and causes the provider to 401.
func (p *Provider) Install(ctx context.Context) error {
	brevitasKey, _ := p.APIKeyValue(ctx)
	return p.EditJSON(p.settingsPath(), func(root map[string]any) error {
		env, _ := root["env"].(map[string]any)
		if env == nil {
			env = map[string]any{}
		}
		env["ANTHROPIC_BASE_URL"] = p.ProxyURL()
		env["ANTHROPIC_CUSTOM_HEADERS"] = "X-Brevitas-Client: claude-code"
		if brevitasKey != "" {
			if v, ok := env["ANTHROPIC_API_KEY"].(string); ok && v == brevitasKey {
				delete(env, "ANTHROPIC_API_KEY")
			}
		}
		root["env"] = env
		return nil
	})
}

// Uninstall restores the original settings.json.
func (p *Provider) Uninstall(ctx context.Context) error { return p.Restore() }

// Validate confirms the base URL points at Brevitas.
func (p *Provider) Validate(ctx context.Context) error {
	return provider.ValidateNestedEquals(p.settingsPath(), []string{"env", "ANTHROPIC_BASE_URL"}, p.ProxyURL())
}

// Status returns a snapshot.
func (p *Provider) Status(ctx context.Context) provider.Status {
	detected := p.Detect(ctx)
	configured := detected && p.Validate(ctx) == nil
	return provider.StatusFor(p.Name(), p.Support(), detected, configured, p.settingsPath(), "")
}
