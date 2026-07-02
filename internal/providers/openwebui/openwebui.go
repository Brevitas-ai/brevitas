// Package openwebui integrates Open WebUI with Brevitas.
//
// Support is Partial: Open WebUI stores its OpenAI connection settings in a
// server-side database and is typically configured via the OPENAI_API_BASE_URL
// environment variable or its admin panel. There is no documented local file
// Brevitas can safely edit, so it detects Open WebUI and provides instructions.
package openwebui

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Brevitas-ai/brevitas/internal/detect"
	"github.com/Brevitas-ai/brevitas/internal/provider"
)

const reason = "Open WebUI keeps connection settings in its database; set the OpenAI " +
	"API base URL via the admin panel or OPENAI_API_BASE_URL."

// Provider integrates Open WebUI.
type Provider struct{ provider.Base }

// New constructs the Open WebUI provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "openwebui", "Open WebUI", provider.SupportPartial, reason)}
}

// Detect looks for Open WebUI's CLI or data directory.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.Executable("open-webui") ||
		detect.Exists(filepath.Join(detect.HomeDir(), ".open-webui")) ||
		detect.Exists(filepath.Join(detect.HomeDir(), ".webui")) ||
		detect.Exists("/app/backend/data/webui.db")
}

// Install returns manual instructions.
func (p *Provider) Install(ctx context.Context) error {
	return &provider.ManualStepError{
		Provider: p.DisplayName(),
		Instructions: fmt.Sprintf(
			"In Open WebUI, Admin Settings > Connections, set the OpenAI API Base URL to %s (or export OPENAI_API_BASE_URL=%s).",
			p.OpenAIBaseURL(), p.OpenAIBaseURL()),
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
