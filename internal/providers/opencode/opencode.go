// Package opencode integrates the OpenCode CLI with Brevitas by setting the
// OpenAI provider baseURL in its documented JSON config.
package opencode

import (
	"context"
	"path/filepath"

	"github.com/Brevitas-ai/brevitas/internal/detect"
	"github.com/Brevitas-ai/brevitas/internal/provider"
)

// Provider integrates OpenCode.
type Provider struct{ provider.Base }

// New constructs the OpenCode provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "opencode", "OpenCode", provider.SupportFull, "")}
}

func (p *Provider) configPath() string {
	return filepath.Join(detect.AppSupportDir("opencode"), "opencode.json")
}

// Detect looks for the OpenCode CLI or its config.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.Executable("opencode") ||
		detect.Exists(p.configPath()) ||
		detect.Exists(filepath.Join(detect.HomeDir(), ".opencode"))
}

// Install sets the OpenAI provider baseURL to the Brevitas proxy.
func (p *Provider) Install(ctx context.Context) error {
	key, _ := p.APIKeyValue(ctx)
	return p.EditJSON(p.configPath(), func(root map[string]any) error {
		prov, _ := root["provider"].(map[string]any)
		if prov == nil {
			prov = map[string]any{}
		}
		openai, _ := prov["openai"].(map[string]any)
		if openai == nil {
			openai = map[string]any{}
		}
		opts, _ := openai["options"].(map[string]any)
		if opts == nil {
			opts = map[string]any{}
		}
		opts["baseURL"] = p.OpenAIBaseURL()
		if key != "" {
			opts["apiKey"] = key
		}
		openai["options"] = opts
		prov["openai"] = openai
		root["provider"] = prov
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
