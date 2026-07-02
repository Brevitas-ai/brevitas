//go:build linux

package keyring

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

func osBackend() string { return "Secret Service (libsecret)" }

// attrs identifies the Brevitas item within the Secret Service collection.
func attrs() []string {
	return []string{"service", Service, "account", Account}
}

func secretTool() (string, error) {
	path, err := exec.LookPath("secret-tool")
	if err != nil {
		return "", ErrUnavailable
	}
	return path, nil
}

func osSet(ctx context.Context, secret string) error {
	bin, err := secretTool()
	if err != nil {
		return err
	}
	args := append([]string{"store", "--label", "Brevitas API Key"}, attrs()...)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = strings.NewReader(secret)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &backendError{op: "secret-tool store", detail: strings.TrimSpace(stderr.String()), err: err}
	}
	return nil
}

func osGet(ctx context.Context) (string, error) {
	bin, err := secretTool()
	if err != nil {
		return "", err
	}
	args := append([]string{"lookup"}, attrs()...)
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		// secret-tool exits non-zero with empty output when the item is absent.
		if errors.As(err, &exitErr) && strings.TrimSpace(stdout.String()) == "" {
			return "", ErrNotFound
		}
		return "", &backendError{op: "secret-tool lookup", detail: strings.TrimSpace(stderr.String()), err: err}
	}
	out := strings.TrimRight(stdout.String(), "\n")
	if out == "" {
		return "", ErrNotFound
	}
	return out, nil
}

func osDelete(ctx context.Context) error {
	bin, err := secretTool()
	if err != nil {
		return err
	}
	args := append([]string{"clear"}, attrs()...)
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return &backendError{op: "secret-tool clear", detail: strings.TrimSpace(stderr.String()), err: err}
	}
	return nil
}
