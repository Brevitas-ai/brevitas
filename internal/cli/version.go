package cli

import (
	"context"

	"github.com/brevitas-systems/brevitas/internal/version"
)

func (a *App) cmdVersion(_ context.Context, _ []string) error {
	a.say("%s", version.String())
	return nil
}
