// Package detect provides best-effort, cross-platform helpers used by
// providers to locate installed AI tools: executables on PATH, config files,
// application-support folders, environment variables, and known install
// paths.
//
// Detection is intentionally forgiving: a false positive is harmless (the
// provider's Validate step will catch it) while helpers never panic and never
// require elevated permissions.
package detect

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// HomeDir returns the current user's home directory, or "" if unknown.
func HomeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// Expand resolves a leading "~" and environment variables in a path.
func Expand(p string) string {
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "~") {
		if h := HomeDir(); h != "" {
			p = filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return os.ExpandEnv(p)
}

// Exists reports whether a file or directory exists at the (expanded) path.
func Exists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(Expand(p))
	return err == nil
}

// AnyExists reports whether any of the given paths exist.
func AnyExists(paths ...string) bool {
	for _, p := range paths {
		if Exists(p) {
			return true
		}
	}
	return false
}

// FirstExisting returns the first path that exists, or "" if none do.
func FirstExisting(paths ...string) string {
	for _, p := range paths {
		if Exists(p) {
			return Expand(p)
		}
	}
	return ""
}

// Executable reports whether an executable with any of the given names is
// found on PATH.
func Executable(names ...string) bool {
	return ExecutablePath(names...) != ""
}

// ExecutablePath returns the resolved path of the first executable found on
// PATH, or "" if none are found.
func ExecutablePath(names ...string) string {
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// EnvSet reports whether any of the named environment variables is set to a
// non-empty value.
func EnvSet(names ...string) bool {
	for _, n := range names {
		if os.Getenv(n) != "" {
			return true
		}
	}
	return false
}

// AppSupportDir returns the platform's per-user application data directory
// for the given application name (used by Electron/VS Code style apps).
func AppSupportDir(app string) string {
	home := HomeDir()
	if home == "" {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", app)
	case "windows":
		if ad := os.Getenv("APPDATA"); ad != "" {
			return filepath.Join(ad, app)
		}
		return filepath.Join(home, "AppData", "Roaming", app)
	default:
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, app)
		}
		return filepath.Join(home, ".config", app)
	}
}

// ConfigCandidates returns common config-file locations for a dotfile-style
// tool, given the base file/dir name (e.g. "aider" -> ~/.aider.conf.yml,
// ~/.config/aider/...).
func ConfigCandidates(rel ...string) []string {
	home := HomeDir()
	var out []string
	for _, r := range rel {
		if home != "" {
			out = append(out, filepath.Join(home, r))
		}
	}
	return out
}

// GUIAppInstalled reports whether a desktop application bundle is present in a
// standard location (macOS .app, Windows Program Files, Linux /opt & desktop
// entries).
func GUIAppInstalled(app string) bool {
	home := HomeDir()
	switch runtime.GOOS {
	case "darwin":
		return AnyExists(
			filepath.Join("/Applications", app+".app"),
			filepath.Join(home, "Applications", app+".app"),
		)
	case "windows":
		var roots []string
		for _, e := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
			if v := os.Getenv(e); v != "" {
				roots = append(roots, filepath.Join(v, app))
			}
		}
		return AnyExists(roots...)
	default:
		return AnyExists(
			filepath.Join("/opt", strings.ToLower(app)),
			filepath.Join("/usr/share/applications", strings.ToLower(app)+".desktop"),
			filepath.Join(home, ".local/share/applications", strings.ToLower(app)+".desktop"),
		)
	}
}

// VSCodeExtensionInstalled reports whether a VS Code (or compatible fork)
// extension directory exists whose name starts with publisher.name.
func VSCodeExtensionInstalled(publisherDotName string) bool {
	home := HomeDir()
	if home == "" {
		return false
	}
	roots := []string{
		filepath.Join(home, ".vscode", "extensions"),
		filepath.Join(home, ".vscode-insiders", "extensions"),
		filepath.Join(home, ".vscode-server", "extensions"),
		filepath.Join(home, ".cursor", "extensions"),
		filepath.Join(home, ".windsurf", "extensions"),
	}
	prefix := strings.ToLower(publisherDotName)
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasPrefix(strings.ToLower(e.Name()), prefix) {
				return true
			}
		}
	}
	return false
}
