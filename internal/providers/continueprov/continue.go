// Package continueprov integrates the Continue extension (VS Code / JetBrains)
// with Brevitas by upserting a model entry that targets the local proxy in the
// documented ~/.continue/config.json file.
package continueprov

import (
	"context"
	"path/filepath"

	"github.com/Brevitas-ai/brevitas/internal/detect"
	"github.com/Brevitas-ai/brevitas/internal/provider"
)

// Provider integrates Continue.
type Provider struct{ provider.Base }

// New constructs the Continue provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "continue", "Continue", provider.SupportFull, "")}
}

func (p *Provider) configPath() string {
	return filepath.Join(detect.HomeDir(), ".continue", "config.json")
}

// Detect looks for the Continue config directory or extension install.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.Exists(filepath.Join(detect.HomeDir(), ".continue")) ||
		detect.VSCodeExtensionInstalled("continue.continue")
}

const modelTitle = "Brevitas (optimized)"

// Install upserts a Brevitas-targeted OpenAI-compatible model. It sets the
// apiBase only; the user supplies their own provider key (via OPENAI_API_KEY
// or by editing the entry), which the proxy forwards through unchanged.
func (p *Provider) Install(ctx context.Context) error {
	return p.EditJSON(p.configPath(), func(root map[string]any) error {
		models, _ := root["models"].([]any)

		entry := map[string]any{
			"title":    modelTitle,
			"provider": "openai",
			"model":    "gpt-4o",
			"apiBase":  p.OpenAIBaseURL(),
			"apiKey":   "OPENAI_API_KEY",
		}

		replaced := false
		for i, m := range models {
			if mm, ok := m.(map[string]any); ok && mm["title"] == modelTitle {
				models[i] = entry
				replaced = true
				break
			}
		}
		if !replaced {
			models = append(models, entry)
		}
		root["models"] = models
		return nil
	})
}

// Uninstall restores the original config.
func (p *Provider) Uninstall(ctx context.Context) error { return p.Restore() }

// Validate confirms the proxy URL appears in the config.
func (p *Provider) Validate(ctx context.Context) error {
	return provider.ValidateJSONAnyContains(p.configPath(), p.OpenAIBaseURL())
}

// Status returns a snapshot.
func (p *Provider) Status(ctx context.Context) provider.Status {
	detected := p.Detect(ctx)
	configured := detected && p.Validate(ctx) == nil
	return provider.StatusFor(p.Name(), p.Support(), detected, configured, p.configPath(), "")
}
