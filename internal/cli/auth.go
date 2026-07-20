package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/Brevitas-ai/brevitas/internal/cloud"
	"github.com/Brevitas-ai/brevitas/internal/keyring"
)

var openBrowser = func(url string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		command = exec.Command("xdg-open", url)
	}
	return command.Start()
}

func (a *App) cmdLogin(ctx context.Context, args []string) error {
	if helpRequested(args) {
		a.printLoginHelp()
		return nil
	}
	a.page("Connect your account", "Authorize BVX and store a revocable device key securely.")
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	keyFlag := fs.String("api-key", "", "Brevitas API key (for CI; otherwise browser login)")
	noOpen := fs.Bool("no-open", false, "print the authorization URL without opening a browser")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *keyFlag != "" {
		return a.storeAPIKey(ctx, *keyFlag)
	}
	return a.loginWithBrowser(ctx, !*noOpen)
}

func (a *App) loginWithBrowser(ctx context.Context, shouldOpen bool) error {
	deviceID, err := a.ensureDeviceIdentity()
	if err != nil {
		return fmt.Errorf("prepare device identity: %w", err)
	}
	device := cloud.Device(deviceID)
	var auth *cloud.DeviceAuthorization
	err = a.withLoading("Starting secure browser login…", func() error {
		var startErr error
		auth, startErr = cloud.StartDeviceAuthorizationFor(ctx, device)
		return startErr
	})
	if err != nil {
		return fmt.Errorf("start browser login: %w", err)
	}
	a.section("Browser authorization")
	a.command(auth.VerificationURIComplete, "Approve this device in your Brevitas dashboard")
	if shouldOpen {
		if err := openBrowser(auth.VerificationURIComplete); err != nil {
			a.warn("Could not open a browser; use the URL above")
		}
	}
	expires := time.Duration(auth.ExpiresIn) * time.Second
	if expires <= 0 {
		expires = 10 * time.Minute
	}
	loginCtx, cancel := context.WithTimeout(ctx, expires)
	defer cancel()
	interval := time.Duration(auth.Interval) * time.Second
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var apiKey string
	err = a.withLoading("Waiting for dashboard approval…", func() error {
		for {
			key, pending, pollErr := cloud.PollDeviceAuthorizationFor(loginCtx, auth.DeviceCode, device)
			if pollErr != nil {
				return fmt.Errorf("finish browser login: %w", pollErr)
			}
			if !pending {
				apiKey = key
				return nil
			}
			select {
			case <-loginCtx.Done():
				return errors.New("browser login expired; run `bvx login` again")
			case <-ticker.C:
			}
		}
	})
	if err != nil {
		return err
	}
	return a.storeAPIKey(ctx, apiKey)
}

func (a *App) ensureDeviceIdentity() (string, error) {
	id, err := a.Cfg.EnsureDeviceID()
	if err != nil {
		return "", err
	}
	if err := a.saveInventoryConfig(); err != nil {
		return "", err
	}
	return id, nil
}

func (a *App) storeAPIKey(ctx context.Context, key string) error {
	if key == "" {
		return errors.New("no API key provided")
	}
	if err := a.withLoading("Saving the API key securely…", func() error {
		return a.Keyring.Set(ctx, key)
	}); err != nil {
		return fmt.Errorf("store key in %s: %w", a.Keyring.Backend(), err)
	}
	a.ok("API key stored in %s", a.Keyring.Backend())
	return nil
}

func (a *App) cmdLogout(ctx context.Context, _ []string) error {
	err := a.withLoading("Removing the stored API key…", func() error {
		return a.Keyring.Delete(ctx)
	})
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
