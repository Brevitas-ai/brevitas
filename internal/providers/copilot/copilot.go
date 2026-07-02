// Package copilot detects GitHub Copilot and reports it as Unsupported.
//
// Why Copilot cannot be proxied (by design, not by omission):
//
//   - Endpoints are not user-configurable. The Copilot extensions hard-code
//     api.githubcopilot.com / *.githubcopilot.com; there is no documented
//     setting to point them at a different base URL.
//   - Auth is short-lived and server-bound. The editor exchanges the user's
//     GitHub OAuth token for a per-session Copilot token minted by GitHub;
//     that token is only accepted by GitHub's own endpoints, so forwarding the
//     traffic elsewhere fails authentication.
//   - Transport is protected. Copilot uses TLS to GitHub's servers; the only
//     way to intercept it would be to install a MITM root certificate or patch
//     the extension — both of which Brevitas explicitly refuses to do (no
//     binary modification, no code injection, no auth hacks).
//
// Brevitas therefore detects Copilot and clearly reports that direct proxying
// is not available, rather than attempting an unsupported workaround.
package copilot

import (
	"context"

	"github.com/Brevitas-ai/brevitas/internal/detect"
	"github.com/Brevitas-ai/brevitas/internal/provider"
)

const reason = "GitHub Copilot uses hard-coded GitHub endpoints with short-lived, " +
	"server-bound tokens over protected TLS. Redirecting it would require MITM " +
	"certificates or binary patching, which Brevitas does not do."

// Provider detects GitHub Copilot.
type Provider struct{ provider.Base }

// New constructs the Copilot provider.
func New(env *provider.Env) provider.Provider {
	return &Provider{Base: provider.NewBase(env, "copilot", "GitHub Copilot", provider.SupportUnsupported, reason)}
}

// Detect looks for the Copilot editor extensions or CLI.
func (p *Provider) Detect(ctx context.Context) bool {
	return detect.VSCodeExtensionInstalled("github.copilot") ||
		detect.VSCodeExtensionInstalled("github.copilot-chat") ||
		detect.Executable("gh") && detect.Exists(detect.Expand("~/.config/gh/hosts.yml"))
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
