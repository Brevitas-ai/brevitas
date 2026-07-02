// Package cline integrates the Cline VS Code extension with Brevitas.
//
// Support is Partial: Cline stores its API-provider configuration (including
// the OpenAI-Compatible base URL) in VS Code's encrypted extension global
// state, which has no documented, safely-editable file. Brevitas detects Cline
// and guides the user to point its "OpenAI Compatible" base URL at the proxy;
// it never edits the encrypted secret store or injects code.
package cline

import (
	"context"
	"fmt"

	"github.com/brevitas-systems/brevitas/internal/detect"
	"github.com/brevitas-systems/brevitas/internal/provider"
)

// Provider integrates Cline.
type Provider struct{ provider.Base }

// New constructs the Cline provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "cline", "Cline", provider.SupportPartial, reason)}
}

const reason = "Cline keeps its API configuration in VS Code's encrypted extension state; " +
	"set the OpenAI-Compatible base URL manually."

// Detect looks for the Cline extension directory.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.VSCodeExtensionInstalled("saoudrizwan.claude-dev") ||
		detect.VSCodeExtensionInstalled("cline")
}

// Install returns manual instructions rather than editing encrypted state.
func (p *Provider) Install(ctx context.Context) error {
	return &provider.ManualStepError{
		Provider: p.DisplayName(),
		Instructions: fmt.Sprintf(
			"In Cline settings choose API Provider \"OpenAI Compatible\", set Base URL to %s and API Key to your Brevitas key.",
			p.OpenAIBaseURL()),
	}
}

// Uninstall is a no-op (Brevitas never modified Cline's state).
func (p *Provider) Uninstall(ctx context.Context) error { return nil }

// Validate cannot confirm encrypted state; it reports the manual requirement.
func (p *Provider) Validate(ctx context.Context) error {
	return &provider.ManualStepError{Provider: p.DisplayName(), Instructions: reason}
}

// Status returns a snapshot (never "configured" — it can't be verified).
func (p *Provider) Status(ctx context.Context) provider.Status {
	return provider.StatusFor(p.Name(), p.Support(), p.Detect(ctx), false, "", reason)
}
