// Package jan integrates the Jan desktop app with Brevitas.
//
// Support is Partial: Jan stores remote-engine configuration in its app-managed
// data folder in a format that is not a documented stable public schema.
// Brevitas detects Jan and guides the user to add a remote OpenAI-compatible
// engine pointed at the proxy, rather than editing app-internal files.
package jan

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/brevitas-systems/brevitas/internal/detect"
	"github.com/brevitas-systems/brevitas/internal/provider"
)

const reason = "Jan stores engine settings in its app data folder; add a remote " +
	"OpenAI-compatible engine pointing at the proxy from within Jan."

// Provider integrates Jan.
type Provider struct{ provider.Base }

// New constructs the Jan provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "jan", "Jan", provider.SupportPartial, reason)}
}

// Detect looks for the Jan app or data directory.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.GUIAppInstalled("Jan") ||
		detect.Exists(filepath.Join(detect.HomeDir(), "jan")) ||
		detect.Exists(filepath.Join(detect.HomeDir(), ".jan")) ||
		detect.Exists(detect.AppSupportDir("Jan"))
}

// Install returns manual instructions.
func (p *Provider) Install(ctx context.Context) error {
	return &provider.ManualStepError{
		Provider: p.DisplayName(),
		Instructions: fmt.Sprintf(
			"In Jan > Settings > Model Providers, add an OpenAI-compatible provider with Base URL %s and your Brevitas key.",
			p.OpenAIBaseURL()),
	}
}

// Uninstall is a no-op.
func (p *Provider) Uninstall(ctx context.Context) error { return nil }

// Validate reports the manual requirement.
func (p *Provider) Validate(ctx context.Context) error {
	return &provider.ManualStepError{Provider: p.DisplayName(), Instructions: reason}
}

// Status returns a snapshot.
func (p *Provider) Status(ctx context.Context) provider.Status {
	return provider.StatusFor(p.Name(), p.Support(), p.Detect(ctx), false, "", reason)
}
