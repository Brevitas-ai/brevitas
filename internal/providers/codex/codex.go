// Package codex integrates the OpenAI Codex CLI with Brevitas by declaring a
// custom model provider in its documented ~/.codex/config.toml and selecting
// it as the default.
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

// Install writes a managed TOML block defining and selecting the Brevitas
// provider. The block is placed at the top of the file so the bare
// `model_provider` key remains valid TOML.
func (p *Provider) Install(ctx context.Context) error {
	// Codex deprecated wire_api = "chat" in favor of "responses"
	// (github.com/openai/codex/discussions/7782). The proxy routes the
	// Responses API (/v1/responses) the same as chat completions.
	// env_key tells Codex which environment variable holds the API key to send
	// (the user's own OpenAI key). Without it Codex sends no credential and the
	// upstream returns 401.
	block := fmt.Sprintf(`model_provider = "brevitas"

[model_providers.brevitas]
name = "Brevitas"
base_url = %q
env_key = "OPENAI_API_KEY"
wire_api = "responses"
http_headers = { "X-Brevitas-Client" = "codex" }`, p.OpenAIBaseURL())
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
