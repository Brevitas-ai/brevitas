// Package codex integrates the OpenAI Codex CLI with Brevitas by routing its
// built-in OpenAI provider through the local proxy.
package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// Install writes a custom Responses provider that reuses Codex's persisted
// OpenAI login. Brevitas currently proxies HTTPS/SSE requests, not WebSocket
// upgrades, so explicitly disabling WebSockets prevents Codex from attempting
// a handshake that the proxy would have to reject before falling back to HTTPS.
// Dotted TOML keeps the provider definition at the root, allowing the managed
// block to remain above any user-owned tables and top-level settings.
func (p *Provider) Install(ctx context.Context) error {
	block := fmt.Sprintf(`model_provider = "brevitas"
model_providers.brevitas = { name = "Brevitas", base_url = %q, wire_api = "responses", requires_openai_auth = true, supports_websockets = false }`, p.OpenAIBaseURL())
	return p.EditManagedBlockAt(p.configPath(), block, true)
}

// Uninstall restores the original config.toml.
func (p *Provider) Uninstall(ctx context.Context) error { return p.Restore() }

// Validate confirms the complete provider configuration is present.
func (p *Provider) Validate(ctx context.Context) error {
	raw, err := os.ReadFile(p.configPath())
	if err != nil {
		return fmt.Errorf("read Codex config: %w", err)
	}
	config := string(raw)
	for _, want := range []string{
		`model_provider = "brevitas"`,
		p.OpenAIBaseURL(),
		`requires_openai_auth = true`,
		`supports_websockets = false`,
	} {
		if !strings.Contains(config, want) {
			return fmt.Errorf("Codex config missing %q", want)
		}
	}
	return nil
}

// Status returns a snapshot.
func (p *Provider) Status(ctx context.Context) provider.Status {
	detected := p.Detect(ctx)
	configured := detected && p.Validate(ctx) == nil
	return provider.StatusFor(p.Name(), p.Support(), detected, configured, p.configPath(), "")
}
