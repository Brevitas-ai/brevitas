package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// Dirs holds the resolved filesystem locations Brevitas uses at runtime.
type Dirs struct {
	// Config is where brevitas keeps its own config.yaml and backups.
	Config string
	// Data holds state such as the service socket path and rollback journal.
	Data string
	// Logs holds proxy and service log files.
	Logs string
	// Cache holds transient data (update checks, detection cache).
	Cache string
}

// ResolveDirs returns platform-appropriate directories, honoring the
// BREVITAS_HOME override which forces every directory under a single root
// (useful for tests and portable installs).
func ResolveDirs() Dirs {
	if home := os.Getenv("BREVITAS_HOME"); home != "" {
		return Dirs{
			Config: filepath.Join(home, "config"),
			Data:   filepath.Join(home, "data"),
			Logs:   filepath.Join(home, "logs"),
			Cache:  filepath.Join(home, "cache"),
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}

	switch runtime.GOOS {
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support", "Brevitas")
		return Dirs{
			Config: base,
			Data:   base,
			Logs:   filepath.Join(home, "Library", "Logs", "Brevitas"),
			Cache:  filepath.Join(home, "Library", "Caches", "Brevitas"),
		}
	case "windows":
		appData := envOr("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		localAppData := envOr("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
		base := filepath.Join(appData, "Brevitas")
		local := filepath.Join(localAppData, "Brevitas")
		return Dirs{
			Config: base,
			Data:   base,
			Logs:   filepath.Join(local, "Logs"),
			Cache:  filepath.Join(local, "Cache"),
		}
	default: // linux and other unix
		cfg := envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		data := envOr("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
		cache := envOr("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
		state := envOr("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
		return Dirs{
			Config: filepath.Join(cfg, "brevitas"),
			Data:   filepath.Join(data, "brevitas"),
			Logs:   filepath.Join(state, "brevitas", "logs"),
			Cache:  filepath.Join(cache, "brevitas"),
		}
	}
}

// EnsureAll creates every directory, returning the first error encountered.
func (d Dirs) EnsureAll() error {
	for _, p := range []string{d.Config, d.Data, d.Logs, d.Cache} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// ConfigFile returns the path to brevitas's own configuration file.
func (d Dirs) ConfigFile() string { return filepath.Join(d.Config, "config.json") }

// BackupDir returns the directory holding provider config backups.
func (d Dirs) BackupDir() string { return filepath.Join(d.Data, "backups") }

// JournalFile returns the rollback journal path.
func (d Dirs) JournalFile() string { return filepath.Join(d.Data, "rollback.json") }

// SocketPath returns the default optimizer/service socket path. On Windows a
// TCP loopback address is used instead (see optimizer package).
func (d Dirs) SocketPath() string { return filepath.Join(d.Data, "brevitas.sock") }

// ProxyLog returns the proxy log file path.
func (d Dirs) ProxyLog() string { return filepath.Join(d.Logs, "proxy.log") }

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
