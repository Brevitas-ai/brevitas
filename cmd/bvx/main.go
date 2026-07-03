// Command bvx is the installer and integration framework CLI for Brevitas
// (named "bvx" to avoid colliding with the brevitas-systems Python CLI). It
// detects AI coding tools, stores a single API key securely,
// configures each tool to route through the local optimization proxy, and
// manages the background service.
//
// The optimization logic lives entirely in the brevitas-systems Python
// package; this binary only handles installation, configuration, and
// lifecycle.
package main

import (
	"os"

	"github.com/Brevitas-ai/brevitas/internal/cli"
)

func main() {
	os.Exit(cli.Main())
}
