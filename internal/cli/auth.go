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
	auth, err := cloud.StartDeviceAuthorizationFor(ctx, device)
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
	a.note("Waiting for dashboard approval…")

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
	for {
		key, pending, err := cloud.PollDeviceAuthorizationFor(loginCtx, auth.DeviceCode, device)
		if err != nil {
			return fmt.Errorf("finish browser login: %w", err)
		}
		if !pending {
			return a.storeAPIKey(ctx, key)
		}
		select {
		case <-loginCtx.Done():
			return errors.New("browser login expired; run `bvx login` again")
		case <-ticker.C:
		}
	}
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
