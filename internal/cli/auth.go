package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/Brevitas-ai/brevitas/internal/keyring"
)

func (a *App) cmdLogin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	keyFlag := fs.String("api-key", "", "Brevitas API key (otherwise prompted)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	key := *keyFlag
	if key == "" {
		var err error
		key, err = a.promptSecret("Enter Brevitas API key: ")
		if err != nil {
			return err
		}
	}
	if key == "" {
		return errors.New("no API key provided")
	}
	if err := a.Keyring.Set(ctx, key); err != nil {
		return fmt.Errorf("store key in %s: %w", a.Keyring.Backend(), err)
	}
	a.ok("API key stored in %s", a.Keyring.Backend())
	return nil
}

func (a *App) cmdLogout(ctx context.Context, _ []string) error {
	err := a.Keyring.Delete(ctx)
	if errors.Is(err, keyring.ErrNotFound) {
		a.say("No API key was stored.")
		return nil
	}
	if err != nil {
		return err
	}
	a.ok("API key removed from %s", a.Keyring.Backend())
	return nil
}
