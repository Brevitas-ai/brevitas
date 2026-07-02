// Package anythingllm integrates AnythingLLM with Brevitas.
//
// Support is Partial: AnythingLLM stores its LLM-provider configuration in a
// server-side database / .env managed by the app. Brevitas detects it and
// guides the user to configure a "Generic OpenAI" provider pointed at the proxy.
package anythingllm

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/Brevitas-ai/brevitas/internal/detect"
	"github.com/Brevitas-ai/brevitas/internal/provider"
)

const reason = "AnythingLLM stores LLM settings in its database; configure a Generic " +
	"OpenAI provider pointing at the proxy from within the app."

// Provider integrates AnythingLLM.
type Provider struct{ provider.Base }

// New constructs the AnythingLLM provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "anythingllm", "AnythingLLM", provider.SupportPartial, reason)}
}

// Detect looks for the AnythingLLM app or storage directory.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.GUIAppInstalled("AnythingLLM") ||
		detect.Executable("anythingllm") ||
		detect.Exists(detect.AppSupportDir("anythingllm-desktop")) ||
		detect.Exists(filepath.Join(detect.HomeDir(), ".anythingllm"))
}

// Install returns manual instructions.
func (p *Provider) Install(ctx context.Context) error {
	return &provider.ManualStepError{
		Provider: p.DisplayName(),
		Instructions: fmt.Sprintf(
			"In AnythingLLM, choose LLM Provider \"Generic OpenAI\", set Base URL to %s and paste your Brevitas key.",
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
