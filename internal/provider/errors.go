package provider

import (
	"errors"
	"fmt"
)

func asManual(err error, target **ManualStepError) bool { return errors.As(err, target) }

// ManualStepError is returned by Install for providers that cannot be
// configured by editing a documented file (their settings live in encrypted
// app state or a proprietary database). It is not a failure: the installer
// reports the instructions to the user rather than aborting.
type ManualStepError struct {
	Provider     string
	Instructions string
}

func (e *ManualStepError) Error() string {
	return fmt.Sprintf("%s requires a manual configuration step: %s", e.Provider, e.Instructions)
}

// IsManualStep reports whether err is a ManualStepError.
func IsManualStep(err error) (*ManualStepError, bool) {
	var m *ManualStepError
	if asManual(err, &m) {
		return m, true
	}
	return nil, false
}
