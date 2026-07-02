package keyring

import "fmt"

// backendError wraps a failure from an underlying credential-store tool while
// preserving the original error for errors.Is/As.
type backendError struct {
	op     string
	detail string
	err    error
}

func (e *backendError) Error() string {
	if e.detail != "" {
		return fmt.Sprintf("keyring %s: %s", e.op, e.detail)
	}
	return fmt.Sprintf("keyring %s: %v", e.op, e.err)
}

func (e *backendError) Unwrap() error { return e.err }
