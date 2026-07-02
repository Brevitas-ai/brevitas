//go:build darwin

package keyring

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

func osBackend() string { return "macOS Keychain" }

func osSet(ctx context.Context, secret string) error {
	// -U updates the item if it already exists; -w reads the secret from the
	// argument. We delete-then-add would race, so rely on -U.
	cmd := exec.CommandContext(ctx, "security", "add-generic-password",
		"-a", Account, "-s", Service, "-w", secret, "-U")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return wrapSecurityErr(err, stderr.String())
	}
	return nil
}

func osGet(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "security", "find-generic-password",
		"-a", Account, "-s", Service, "-w")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "could not be found") {
			return "", ErrNotFound
		}
		return "", wrapSecurityErr(err, stderr.String())
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

func osDelete(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "security", "delete-generic-password",
		"-a", Account, "-s", Service)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "could not be found") {
			return ErrNotFound
		}
		return wrapSecurityErr(err, stderr.String())
	}
	return nil
}

func wrapSecurityErr(err error, stderr string) error {
	if stderr = strings.TrimSpace(stderr); stderr != "" {
		return &backendError{op: "security", detail: stderr, err: err}
	}
	return &backendError{op: "security", err: err}
}
