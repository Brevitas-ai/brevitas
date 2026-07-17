package cli

import (
	"context"

	"github.com/Brevitas-ai/brevitas/internal/version"
)

func (a *App) cmdVersion(_ context.Context, _ []string) error {
	if !colorEnabled(a.Out) {
		a.say("%s", version.String())
		return nil
	}
	a.page("Version", "Installed BVX build information.")
	a.metric("Build", version.String(), ansiCyan)
	return nil
}
