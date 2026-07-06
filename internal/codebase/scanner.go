// Package codebase integrates Brevitas into a project's own agent code (as
// opposed to interactive AI coding tools, which the providers package handles).
//
// The end-to-end flow this package will drive:
//
//  1. Scan <repo> for LLM API call sites — OpenAI/Anthropic/Google SDKs and
//     raw HTTP — and the API keys/models each one uses.
//  2. Recommend a per-call strategy (optimize vs lossless).
//  3. Wire Brevitas between the agents and the model so the brevitas
//     token-efficiency model reduces tokens on every provider call.
//
// TODO(brevitas): The scanner itself is being built separately and will ship
// as its own pip package (working name: `brevitas-codebase`). Once available,
// Scan should invoke that package (via the configured Python interpreter, the
// same way internal/optimizer locates brevitas-systems) instead of returning
// ErrNotAvailable. Keep this Go-side surface stable so the CLI does not change
// when the scanner lands.
package codebase

import (
	"context"
	"errors"
)

// ErrNotAvailable is returned until the internal codebase scanner ships.
var ErrNotAvailable = errors.New("codebase scanner not available yet")

// CallSite describes one detected LLM call the scanner found.
type CallSite struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Provider string `json:"provider"` // "openai" | "anthropic" | "google"
	Model    string `json:"model,omitempty"`
	// Strategy is the recommended handling: "optimize" or "lossless".
	Strategy string `json:"strategy,omitempty"`
	// KeyEnv is the environment variable the call reads its API key from.
	KeyEnv string `json:"key_env,omitempty"`
}

// Result is the outcome of scanning a repository.
type Result struct {
	Repo      string     `json:"repo"`
	CallSites []CallSite `json:"call_sites"`
	// Wrote lists files the scanner modified (when apply is requested).
	Wrote []string `json:"wrote,omitempty"`
}

// Options controls a scan.
type Options struct {
	// Apply, when true, wires Brevitas into the detected call sites; otherwise
	// the scan is read-only (a report/dry-run).
	Apply bool
	// PythonBin is the interpreter used to run the scanner pip package.
	PythonBin string
}

// Scanner scans a repository and (optionally) wires Brevitas in.
type Scanner interface {
	Scan(ctx context.Context, repo string, opts Options) (*Result, error)
}

// New returns the default Scanner. Today this is the stub scanner; once the
// internal pip package exists, New will return a scanner that shells out to it.
func New() Scanner { return stubScanner{} }

type stubScanner struct{}

func (stubScanner) Scan(ctx context.Context, repo string, opts Options) (*Result, error) {
	return nil, ErrNotAvailable
}
