// Package version exposes build metadata for the Brevitas CLI and proxy.
//
// Values are overridden at build time via -ldflags, e.g.:
//
//	go build -ldflags "-X github.com/Brevitas-ai/brevitas/internal/version.Version=1.2.3"
package version

import (
	"fmt"
	"runtime"
)

var (
	// Version is the semantic version of the Brevitas installer.
	Version = "0.1.0-dev"
	// Commit is the git commit the binary was built from.
	Commit = "unknown"
	// Date is the build date in RFC3339 format.
	Date = "unknown"

	// MinSystemsVersion is the minimum supported version of the
	// brevitas-systems Python package that this installer integrates with.
	MinSystemsVersion = "0.1.0"

	// PinnedSystemsVersion is the exact brevitas-systems version this installer
	// installs and upgrades to. Pinning keeps the proxy and the optimization
	// brain in lockstep.
	PinnedSystemsVersion = "0.9.9"
)

// SystemsPipSpec returns the pip requirement specifier for the pinned
// brevitas-systems version, e.g. "brevitas-systems==0.9.9".
func SystemsPipSpec() string {
	return "brevitas-systems==" + PinnedSystemsVersion
}

// String returns a human-readable version summary.
func String() string {
	return fmt.Sprintf("brevitas %s (commit %s, built %s, %s/%s, %s)",
		Version, Commit, Date, runtime.GOOS, runtime.GOARCH, runtime.Version())
}

// UserAgent returns a value suitable for the HTTP User-Agent header.
func UserAgent() string {
	return fmt.Sprintf("brevitas/%s (%s/%s)", Version, runtime.GOOS, runtime.GOARCH)
}
