// Package windsurf integrates the Windsurf editor (Codeium) with Brevitas.
//
// Support is Unsupported: Windsurf's AI features route through Codeium's
// authenticated servers and it exposes no documented base-URL override or
// editable endpoint configuration. Redirecting its traffic would require
// intercepting an authenticated, provider-managed connection, which Brevitas
// will not do. Windsurf is detected and reported, but not configured.
package windsurf

import (
	"context"

	"github.com/brevitas-systems/brevitas/internal/detect"
	"github.com/brevitas-systems/brevitas/internal/provider"
)

const reason = "Windsurf routes AI traffic through Codeium's authenticated servers " +
	"and offers no documented endpoint override; it cannot be proxied without " +
	"breaking authentication."

// Provider integrates Windsurf.
type Provider struct{ provider.Base }

// New constructs the Windsurf provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "windsurf", "Windsurf", provider.SupportUnsupported, reason)}
}

// Detect looks for the Windsurf application or config directory.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.GUIAppInstalled("Windsurf") ||
		detect.Executable("windsurf") ||
		detect.Exists(detect.Expand("~/.codeium/windsurf")) ||
		detect.Exists(detect.Expand("~/.windsurf"))
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
