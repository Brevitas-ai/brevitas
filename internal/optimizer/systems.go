package optimizer

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Systems inspects the locally installed brevitas-systems Python package. It
// is used by `brevitas doctor` and `brevitas update` — it never runs
// optimization, only version/health probes and pip management.
type Systems struct {
	// PythonBin is the interpreter used (e.g. "python3").
	PythonBin string
}

// NewSystems builds a Systems probe for the given interpreter.
func NewSystems(pythonBin string) *Systems {
	if pythonBin == "" {
		pythonBin = "python3"
	}
	return &Systems{PythonBin: pythonBin}
}

// Installed reports whether the brevitas-systems package can be imported.
func (s *Systems) Installed(ctx context.Context) bool {
	_, err := s.Version(ctx)
	return err == nil
}

// Version returns the installed brevitas-systems version string.
//
// The pip distribution is named "brevitas-systems" while its importable module
// is "brevitas", so we read the version from package metadata (which also
// avoids importing the package just to check it exists).
func (s *Systems) Version(ctx context.Context) (string, error) {
	out, err := s.run(ctx, "-c", "import importlib.metadata as m; print(m.version('brevitas-systems'))")
	if err != nil {
		return "", fmt.Errorf("brevitas-systems not installed for %s: %w", s.PythonBin, err)
	}
	return strings.TrimSpace(out), nil
}

// LatestAvailable queries pip for the newest version on the index.
func (s *Systems) LatestAvailable(ctx context.Context) (string, error) {
	// `pip index versions` prints "Available versions: x, y, z".
	out, err := s.run(ctx, "-m", "pip", "index", "versions", "brevitas-systems")
	if err != nil {
		return "", fmt.Errorf("query pip index: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Available versions:") {
			rest := strings.TrimPrefix(line, "Available versions:")
			parts := strings.Split(rest, ",")
			if len(parts) > 0 {
				return strings.TrimSpace(parts[0]), nil
			}
		}
	}
	return "", fmt.Errorf("could not parse pip index output")
}

// Upgrade runs pip install --upgrade for brevitas-systems.
func (s *Systems) Upgrade(ctx context.Context) error {
	_, err := s.run(ctx, "-m", "pip", "install", "--upgrade", "brevitas-systems")
	if err != nil {
		return fmt.Errorf("upgrade brevitas-systems: %w", err)
	}
	return nil
}

func (s *Systems) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, s.PythonBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", fmt.Errorf("%w: %s", err, detail)
		}
		return "", err
	}
	return stdout.String(), nil
}

// CompareVersions returns -1, 0, or +1 comparing dotted numeric versions a and
// b (e.g. "1.2.0" vs "1.10.0"). Non-numeric suffixes are ignored. It is a
// deliberately small, dependency-free semver-ish comparator.
func CompareVersions(a, b string) int {
	as := splitVersion(a)
	bs := splitVersion(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func splitVersion(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// Drop any pre-release/build suffix.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, part := range strings.Split(v, ".") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}
