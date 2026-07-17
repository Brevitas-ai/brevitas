// Package codex integrates the OpenAI Codex CLI with Brevitas by routing its
// built-in OpenAI provider through the local proxy.
package codex

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Brevitas-ai/brevitas/internal/detect"
	"github.com/Brevitas-ai/brevitas/internal/provider"
)

// Provider integrates the Codex CLI.
type Provider struct{ provider.Base }

// New constructs the Codex provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "codex", "Codex CLI", provider.SupportFull, "")}
}

func (p *Provider) codexDir() string   { return filepath.Join(detect.HomeDir(), ".codex") }
func (p *Provider) configPath() string { return filepath.Join(p.codexDir(), "config.toml") }

// Detect looks for the Codex CLI or its config directory.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.Executable("codex") || detect.Exists(p.codexDir())
}

// Install writes the documented built-in-provider proxy setting. Keeping the
// built-in provider is important: Codex can continue using its persisted
// ChatGPT or API-key login instead of requiring a shell-only environment key.
// The block is placed at the top because openai_base_url is a top-level key.
func (p *Provider) Install(ctx context.Context) error {
	block := fmt.Sprintf(`openai_base_url = %q`, p.OpenAIBaseURL())
	return p.EditManagedBlockAt(p.configPath(), block, true)
}

// Uninstall restores the original config.toml.
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
