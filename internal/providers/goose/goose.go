// Package goose integrates Block's Goose agent with Brevitas by setting the
// OpenAI host in its documented ~/.config/goose/config.yaml.
package goose

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/brevitas-systems/brevitas/internal/detect"
	"github.com/brevitas-systems/brevitas/internal/provider"
)

// Provider integrates Goose.
type Provider struct{ provider.Base }

// New constructs the Goose provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "goose", "Goose", provider.SupportFull, "")}
}

func (p *Provider) configPath() string {
	return filepath.Join(detect.HomeDir(), ".config", "goose", "config.yaml")
}

// Detect looks for the Goose CLI or its config file.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.Executable("goose") || detect.Exists(p.configPath())
}

// Install appends a managed YAML block pointing Goose's OpenAI host at Brevitas.
func (p *Provider) Install(ctx context.Context) error {
	block := fmt.Sprintf("GOOSE_PROVIDER: openai\nOPENAI_HOST: %q\nOPENAI_BASE_PATH: v1/chat/completions", p.ProxyURL())
	return p.EditManagedBlock(p.configPath(), block)
}

// Uninstall restores the original config.
func (p *Provider) Uninstall(ctx context.Context) error { return p.Restore() }

// Validate confirms the proxy URL is referenced.
func (p *Provider) Validate(ctx context.Context) error {
	return provider.ValidateFileContains(p.configPath(), p.ProxyURL())
}

// Status returns a snapshot.
func (p *Provider) Status(ctx context.Context) provider.Status {
	detected := p.Detect(ctx)
	configured := detected && p.Validate(ctx) == nil
	return provider.StatusFor(p.Name(), p.Support(), detected, configured, p.configPath(), "")
}
