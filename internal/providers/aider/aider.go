// Package aider integrates the Aider CLI with Brevitas by setting
// openai-api-base in its documented ~/.aider.conf.yml.
package aider

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Brevitas-ai/brevitas/internal/detect"
	"github.com/Brevitas-ai/brevitas/internal/provider"
)

// Provider integrates Aider.
type Provider struct{ provider.Base }

// New constructs the Aider provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "aider", "Aider", provider.SupportFull, "")}
}

func (p *Provider) configPath() string {
	return filepath.Join(detect.HomeDir(), ".aider.conf.yml")
}

// Detect looks for the Aider CLI or its config file.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.Executable("aider") ||
		detect.Exists(p.configPath()) ||
		detect.Exists(filepath.Join(detect.HomeDir(), ".aider"))
}

// Install appends a managed YAML block pointing Aider at the proxy.
func (p *Provider) Install(ctx context.Context) error {
	key, _ := p.APIKeyValue(ctx)
	block := fmt.Sprintf("openai-api-base: %q\nopenai-api-key: %q", p.OpenAIBaseURL(), key)
	return p.EditManagedBlock(p.configPath(), block)
}

// Uninstall restores the original config.
func (p *Provider) Uninstall(ctx context.Context) error { return p.Restore() }

// Validate confirms the proxy URL is referenced.
func (p *Provider) Validate(ctx context.Context) error {
	return provider.ValidateFileContains(p.configPath(), p.OpenAIBaseURL())
}

// Status returns a snapshot.
func (p *Provider) Status(ctx context.Context) provider.Status {
	detected := p.Detect(ctx)
	configured := detected && p.Validate(ctx) == nil
	return provider.StatusFor(p.Name(), p.Support(), detected, configured, p.configPath(), "")
}
