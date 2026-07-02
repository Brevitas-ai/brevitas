// Package vscodeopenai integrates the "vscode-openai" VS Code extension with
// Brevitas by setting its documented base-URL setting in the user settings.json.
package vscodeopenai

import (
	"context"
	"path/filepath"

	"github.com/brevitas-systems/brevitas/internal/detect"
	"github.com/brevitas-systems/brevitas/internal/provider"
)

// Provider integrates the vscode-openai extension.
type Provider struct{ provider.Base }

// New constructs the vscode-openai provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "vscode-openai", "VS Code OpenAI", provider.SupportFull, "")}
}

func (p *Provider) settingsPath() string {
	return filepath.Join(detect.AppSupportDir("Code"), "User", "settings.json")
}

// Detect looks for the installed extension directory.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.VSCodeExtensionInstalled("andrewbutson.vscode-openai") ||
		detect.VSCodeExtensionInstalled("vscode-openai")
}

// Install writes the base URL into the VS Code user settings.json.
func (p *Provider) Install(ctx context.Context) error {
	key, _ := p.APIKeyValue(ctx)
	return p.EditJSON(p.settingsPath(), func(root map[string]any) error {
		root["vscode-openai.baseUrl"] = p.OpenAIBaseURL()
		root["vscode-openai.serviceProvider"] = "OpenAI"
		if key != "" {
			root["vscode-openai.apiKey"] = key
		}
		return nil
	})
}

// Uninstall restores the original settings.json.
func (p *Provider) Uninstall(ctx context.Context) error { return p.Restore() }

// Validate confirms the base URL points at Brevitas.
func (p *Provider) Validate(ctx context.Context) error {
	return provider.ValidateNestedEquals(p.settingsPath(), []string{"vscode-openai.baseUrl"}, p.OpenAIBaseURL())
}

// Status returns a snapshot.
func (p *Provider) Status(ctx context.Context) provider.Status {
	detected := p.Detect(ctx)
	configured := detected && p.Validate(ctx) == nil
	return provider.StatusFor(p.Name(), p.Support(), detected, configured, p.settingsPath(), "")
}
