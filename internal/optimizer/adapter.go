package optimizer

import (
	"context"
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AdapterScript is the Python adapter that bridges the Go proxy's socket
// protocol to the brevitas-systems package. It is embedded so the CLI can
// launch the optimizer without shipping a separate file.
//
//go:embed brevitas_optimizer.py
var AdapterScript []byte

// WriteAdapter writes the embedded adapter to dir and returns its path.
func WriteAdapter(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "brevitas_optimizer.py")
	if err := os.WriteFile(path, AdapterScript, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// DetectPython returns the ABSOLUTE path of the first interpreter that can
// import the brevitas package. Returning an absolute path is essential: the
// optimizer runs as a background service (launchd/systemd) with a minimal PATH
// that omits conda, Homebrew, and python.org, so a bare "python3" would resolve
// to a different interpreter than the interactive shell used at install time.
// We therefore also probe common absolute install locations explicitly.
func DetectPython(ctx context.Context, preferred string) string {
	candidates := []string{preferred, "python3", "python"}

	home, _ := os.UserHomeDir()
	globs := []string{
		"/Library/Frameworks/Python.framework/Versions/*/bin/python3",
		filepath.Join(home, "anaconda3", "bin", "python3"),
		filepath.Join(home, "miniconda3", "bin", "python3"),
		filepath.Join(home, "miniforge3", "bin", "python3"),
		filepath.Join(home, "mambaforge", "bin", "python3"),
		"/opt/anaconda3/bin/python3",
		"/opt/miniconda3/bin/python3",
		"/opt/homebrew/bin/python3",
		"/usr/local/bin/python3",
	}
	// Honor an active conda env's interpreter too (CONDA_PREFIX is exported in
	// the interactive shell that runs install, even if not under launchd).
	if p := os.Getenv("CONDA_PREFIX"); p != "" {
		candidates = append(candidates, filepath.Join(p, "bin", "python3"))
	}
	for _, g := range globs {
		if strings.Contains(g, "*") {
			m, _ := filepath.Glob(g)
			candidates = append(candidates, m...)
		} else {
			candidates = append(candidates, g)
		}
	}

	seen := map[string]bool{}
	for _, c := range candidates {
		abs := resolvePython(c)
		if abs == "" || seen[abs] {
			continue
		}
		seen[abs] = true
		if canImportBrevitas(ctx, abs) {
			return abs
		}
	}
	return ""
}

// resolvePython turns a candidate (name or path) into an existing absolute path.
func resolvePython(c string) string {
	if c == "" {
		return ""
	}
	if filepath.IsAbs(c) {
		if _, err := os.Stat(c); err == nil {
			return c
		}
		return ""
	}
	if p, err := exec.LookPath(c); err == nil {
		return p
	}
	return ""
}

func canImportBrevitas(ctx context.Context, python string) bool {
	if _, err := exec.LookPath(python); err != nil {
		// May be an absolute path; LookPath still validates it.
		if _, statErr := os.Stat(python); statErr != nil {
			return false
		}
	}
	cmd := exec.CommandContext(ctx, python, "-c", "import brevitas; brevitas.optimize_prompt")
	return cmd.Run() == nil
}
