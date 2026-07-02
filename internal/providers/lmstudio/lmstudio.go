// Package lmstudio detects LM Studio.
//
// Support is Unsupported: LM Studio is itself a local, OpenAI-compatible
// inference server — it serves models on localhost rather than sending
// requests to an external LLM provider. There is no outbound provider call for
// Brevitas to optimize or redirect. (LM Studio can instead be added as a
// Brevitas *upstream*; that is a configuration choice, not a proxied client.)
package lmstudio

import (
	"context"
	"path/filepath"

	"github.com/brevitas-systems/brevitas/internal/detect"
	"github.com/brevitas-systems/brevitas/internal/provider"
)

const reason = "LM Studio is a local inference server (an upstream), not a client " +
	"that calls an external provider, so there is no request to redirect."

// Provider detects LM Studio.
type Provider struct{ provider.Base }

// New constructs the LM Studio provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "lmstudio", "LM Studio", provider.SupportUnsupported, reason)}
}

// Detect looks for the LM Studio app, CLI, or data directory.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.GUIAppInstalled("LM Studio") ||
		detect.Executable("lms", "lm-studio") ||
		detect.Exists(filepath.Join(detect.HomeDir(), ".lmstudio")) ||
		detect.Exists(filepath.Join(detect.HomeDir(), ".cache", "lm-studio"))
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
