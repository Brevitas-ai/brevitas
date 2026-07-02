// Package geminicli integrates Google's Gemini CLI with Brevitas.
//
// Support is Partial: the Gemini CLI authenticates against Google's Code
// Assist / Generative Language endpoints and only exposes a base-URL override
// through the GOOGLE_GEMINI_BASE_URL environment variable (there is no
// documented settings.json key for it). Brevitas writes that variable to the
// CLI's ~/.gemini/.env file; the user must start a new shell for it to take
// effect. Brevitas never touches Google OAuth credentials.
package geminicli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Brevitas-ai/brevitas/internal/detect"
	"github.com/Brevitas-ai/brevitas/internal/provider"
)

const partialReason = "Gemini CLI only supports a base-URL override via the " +
	"GOOGLE_GEMINI_BASE_URL environment variable; Brevitas sets it in ~/.gemini/.env " +
	"but a new shell session is required."

// Provider integrates the Gemini CLI.
type Provider struct{ provider.Base }

// New constructs the Gemini CLI provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "gemini-cli", "Gemini CLI", provider.SupportPartial, partialReason)}
}

func (p *Provider) geminiDir() string { return filepath.Join(detect.HomeDir(), ".gemini") }
func (p *Provider) envPath() string   { return filepath.Join(p.geminiDir(), ".env") }

// Detect looks for the Gemini CLI or its config directory.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.Executable("gemini") || detect.Exists(p.geminiDir())
}

// Install writes a managed dotenv block pointing the CLI at the proxy.
func (p *Provider) Install(ctx context.Context) error {
	key, _ := p.APIKeyValue(ctx)
	block := fmt.Sprintf("GOOGLE_GEMINI_BASE_URL=%s\nGEMINI_API_KEY=%s", p.ProxyURL(), key)
	return p.EditManagedBlock(p.envPath(), block)
}

// Uninstall restores the original .env.
func (p *Provider) Uninstall(ctx context.Context) error { return p.Restore() }

// Validate confirms the proxy URL is referenced.
func (p *Provider) Validate(ctx context.Context) error {
	return provider.ValidateFileContains(p.envPath(), p.ProxyURL())
}

// Status returns a snapshot.
func (p *Provider) Status(ctx context.Context) provider.Status {
	detected := p.Detect(ctx)
	configured := detected && p.Validate(ctx) == nil
	return provider.StatusFor(p.Name(), p.Support(), detected, configured, p.envPath(), partialReason)
}
