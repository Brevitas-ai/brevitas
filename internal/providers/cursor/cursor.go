// Package cursor integrates the Cursor editor with Brevitas.
//
// Support is Partial: Cursor's "Override OpenAI Base URL" setting is stored in
// the editor's encrypted application state (not a documented JSON file), and
// Cursor's agent features additionally route through Cursor's own servers.
// Brevitas detects Cursor and guides the user to set the override in
// Settings > Models; it does not modify Cursor's state database or binaries.
package cursor

import (
	"context"
	"fmt"

	"github.com/brevitas-systems/brevitas/internal/detect"
	"github.com/brevitas-systems/brevitas/internal/provider"
)

// Provider integrates Cursor.
type Provider struct{ provider.Base }

// New constructs the Cursor provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "cursor", "Cursor", provider.SupportPartial, reason)}
}

const reason = "Cursor stores its OpenAI base-URL override in encrypted app state; " +
	"set it manually under Settings > Models."

// Detect looks for the Cursor application or its config directory.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.GUIAppInstalled("Cursor") ||
		detect.Executable("cursor") ||
		detect.Exists(detect.Expand("~/.cursor"))
}

// Install returns manual instructions rather than editing encrypted state.
func (p *Provider) Install(ctx context.Context) error {
	return &provider.ManualStepError{
		Provider: p.DisplayName(),
		Instructions: fmt.Sprintf(
			"In Cursor open Settings > Models, enable \"Override OpenAI Base URL\", set it to %s, and paste your Brevitas key.",
			p.OpenAIBaseURL()),
	}
}

// Uninstall is a no-op (Brevitas never modified Cursor's state).
func (p *Provider) Uninstall(ctx context.Context) error { return nil }

// Validate reports the manual requirement.
func (p *Provider) Validate(ctx context.Context) error {
	return &provider.ManualStepError{Provider: p.DisplayName(), Instructions: reason}
}

// Status returns a snapshot.
func (p *Provider) Status(ctx context.Context) provider.Status {
	return provider.StatusFor(p.Name(), p.Support(), p.Detect(ctx), false, "", reason)
}
