// Package ollama detects Ollama.
//
// Support is Unsupported: Ollama runs models locally and does not forward
// requests to an external LLM provider, so there is nothing for Brevitas to
// redirect. Like LM Studio, it is an upstream server rather than a client.
package ollama

import (
	"context"
	"path/filepath"

	"github.com/brevitas-systems/brevitas/internal/detect"
	"github.com/brevitas-systems/brevitas/internal/provider"
)

const reason = "Ollama serves models locally and makes no external provider calls, " +
	"so there is no request to redirect."

// Provider detects Ollama.
type Provider struct{ provider.Base }

// New constructs the Ollama provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "ollama", "Ollama", provider.SupportUnsupported, reason)}
}

// Detect looks for the Ollama CLI, app, or data directory.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.Executable("ollama") ||
		detect.GUIAppInstalled("Ollama") ||
		detect.Exists(filepath.Join(detect.HomeDir(), ".ollama")) ||
		detect.EnvSet("OLLAMA_HOST")
}

// Install is unavailable for an unsupported provider.
func (p *Provider) Install(ctx context.Context) error {
	return &provider.ManualStepError{Provider: p.DisplayName(), Instructions: reason}
}

// Uninstall is a no-op.
func (p *Provider) Uninstall(ctx context.Context) error { return nil }

// Validate always reports the unsupported reason.
func (p *Provider) Validate(ctx context.Context) error {
	return &provider.ManualStepError{Provider: p.DisplayName(), Instructions: reason}
}

// Status returns a snapshot.
func (p *Provider) Status(ctx context.Context) provider.Status {
	return p.UnsupportedStatus(p.Detect(ctx))
}
