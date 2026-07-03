package optimizer

import (
	"context"
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// DetectPython returns the first interpreter that can import the brevitas
// package, checking the preferred candidate first, then common names and the
// python.org framework locations. Returns "" if none work.
func DetectPython(ctx context.Context, preferred string) string {
	candidates := []string{preferred, "python3", "python"}
	if runtime.GOOS == "darwin" {
		// python.org framework builds are a very common place for pip packages.
		matches, _ := filepath.Glob("/Library/Frameworks/Python.framework/Versions/*/bin/python3")
		candidates = append(candidates, matches...)
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		if canImportBrevitas(ctx, c) {
			return c
		}
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
