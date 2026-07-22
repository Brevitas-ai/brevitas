// Package config defines Brevitas's own configuration model, filesystem
// layout, and safe load/save helpers. It is intentionally free of any
// optimization logic — that lives in the brevitas-systems Python package.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// DefaultProxyPort is the loopback port the proxy binds to. AI tools are
// reconfigured to send their traffic here.
const DefaultProxyPort = 8080

// OpenAIChatGPTUpstreamKey identifies the Codex backend used by ChatGPT-plan
// authentication. It is separate from the Platform API upstream because a
// ChatGPT access token is not valid at api.openai.com.
const OpenAIChatGPTUpstreamKey = "openai_chatgpt"

const defaultOpenAIChatGPTUpstream = "https://chatgpt.com/backend-api/codex"

// Config is Brevitas's persisted configuration. It never contains the API
// key — that is stored in the OS credential store (see internal/keyring).
type Config struct {
	// SchemaVersion allows forward-compatible migrations.
	SchemaVersion int `json:"schema_version"`

	// Proxy holds proxy listener settings.
	Proxy ProxyConfig `json:"proxy"`

	// Optimizer configures how the Go proxy reaches brevitas-systems.
	Optimizer OptimizerConfig `json:"optimizer"`

	// Upstreams maps a provider family to the real upstream base URL that
	// optimized requests are forwarded to.
	Upstreams map[string]string `json:"upstreams"`

	// EnabledProviders lists provider names the user has configured.
	EnabledProviders []string `json:"enabled_providers"`

	// Inventory contains only pseudonymous device and installation metadata.
	// Credentials remain exclusively in the OS keyring; repository paths,
	// remotes, source, prompts, and provider keys are never persisted here.
	Inventory InventoryConfig `json:"inventory"`

	// UpdatedAt records the last time the config was written.
	UpdatedAt time.Time `json:"updated_at"`
}

// InventoryConfig is the local, non-secret state needed to make AgentMap
// installations idempotent and send periodic presence heartbeats.
type InventoryConfig struct {
	DeviceID      string               `json:"device_id"`
	Installations []InstallationConfig `json:"installations,omitempty"`
}

// InstallationConfig identifies one repository/environment pair without
// retaining its absolute path or Git remote.
type InstallationConfig struct {
	ID                    string    `json:"id"`
	RepositoryID          string    `json:"repository_id"`
	RepositoryLabel       string    `json:"repository_label"`
	Environment           string    `json:"environment"`
	Registered            bool      `json:"registered"`
	HeartbeatIntervalSecs int       `json:"heartbeat_interval_seconds,omitempty"`
	LastHeartbeatAt       time.Time `json:"last_heartbeat_at,omitempty"`
}

// ProxyConfig configures the local HTTP proxy.
type ProxyConfig struct {
	Host           string        `json:"host"`
	Port           int           `json:"port"`
	ReadTimeout    time.Duration `json:"read_timeout"`
	WriteTimeout   time.Duration `json:"write_timeout"`
	RequestTimeout time.Duration `json:"request_timeout"`
	MaxRetries     int           `json:"max_retries"`
	MaxBodyBytes   int64         `json:"max_body_bytes"`
	// UpstreamAuth controls how the proxy authenticates to the upstream:
	//   "passthrough" (default) — forward the tool's own provider credentials
	//                             unchanged; Brevitas only optimizes.
	//   "inject"                — add the stored Brevitas key as X-Brevitas-Key
	//                             while preserving provider authentication.
	UpstreamAuth string `json:"upstream_auth"`
}

// OptimizerConfig configures the connection to the long-running
// brevitas-systems optimization service.
//
// The architectural recommendation is honored here: rather than spawning a
// Python interpreter per request, the Go proxy talks to a persistent
// brevitas-systems process over a Unix domain socket (or loopback TCP on
// Windows). A subprocess fallback exists only for diagnostics.
type OptimizerConfig struct {
	// Transport is "unix" or "tcp".
	Transport string `json:"transport"`
	// Address is the socket path (unix) or host:port (tcp).
	Address string `json:"address"`
	// PythonBin is the interpreter used to launch/verify brevitas-systems.
	PythonBin string `json:"python_bin"`
	// StartTimeout bounds how long we wait for the service to accept
	// connections after launch.
	StartTimeout time.Duration `json:"start_timeout"`
	// CallTimeout bounds a single optimize call.
	CallTimeout time.Duration `json:"call_timeout"`
}

// Default returns a Config populated with sane defaults for the current host.
func Default() *Config {
	dirs := ResolveDirs()

	optAddr := dirs.SocketPath()
	transport := "unix"
	if runtime.GOOS == "windows" {
		transport = "tcp"
		optAddr = "127.0.0.1:8765"
	}

	return &Config{
		SchemaVersion: 1,
		Proxy: ProxyConfig{
			Host:           "127.0.0.1",
			Port:           DefaultProxyPort,
			ReadTimeout:    30 * time.Second,
			WriteTimeout:   0, // 0 = no write deadline, required for streaming/SSE
			RequestTimeout: 10 * time.Minute,
			MaxRetries:     2,
			MaxBodyBytes:   64 << 20, // 64 MiB
			UpstreamAuth:   "passthrough",
		},
		Optimizer: OptimizerConfig{
			Transport:    transport,
			Address:      optAddr,
			PythonBin:    defaultPython(),
			StartTimeout: 20 * time.Second,
			CallTimeout:  60 * time.Second,
		},
		Upstreams: map[string]string{
			"openai":                 "https://api.openai.com",
			OpenAIChatGPTUpstreamKey: defaultOpenAIChatGPTUpstream,
			"anthropic":              "https://api.anthropic.com",
			"google":                 "https://generativelanguage.googleapis.com",
		},
		EnabledProviders: []string{},
		UpdatedAt:        time.Now().UTC(),
	}
}

func defaultPython() string {
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

// Addr returns the host:port the proxy listens on.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Proxy.Host, c.Proxy.Port)
}

// ProxyURL returns the loopback URL that providers are pointed at.
func (c *Config) ProxyURL() string {
	return fmt.Sprintf("http://%s:%d", c.Proxy.Host, c.Proxy.Port)
}

// Load reads the config from disk, returning defaults if it does not exist.
func Load() (*Config, error) {
	return LoadFrom(ResolveDirs().ConfigFile())
}

// LoadFrom reads config from a specific path.
func LoadFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg := Default()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	// Config files written before ChatGPT-plan routing existed replace the
	// default upstream map during JSON decoding. Backfill only the new key so
	// existing user overrides remain untouched.
	if cfg.Upstreams == nil {
		cfg.Upstreams = map[string]string{}
	}
	if _, ok := cfg.Upstreams[OpenAIChatGPTUpstreamKey]; !ok {
		cfg.Upstreams[OpenAIChatGPTUpstreamKey] = defaultOpenAIChatGPTUpstream
	}
	return cfg, nil
}

// Save atomically writes the config to the default location.
func (c *Config) Save() error {
	return c.SaveTo(ResolveDirs().ConfigFile())
}

// SaveTo atomically writes the config to path, creating parent dirs.
func (c *Config) SaveTo(path string) error {
	c.UpdatedAt = time.Now().UTC()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit config: %w", err)
	}
	return nil
}

// AddProvider records a provider as enabled (idempotent).
func (c *Config) AddProvider(name string) {
	for _, p := range c.EnabledProviders {
		if p == name {
			return
		}
	}
	c.EnabledProviders = append(c.EnabledProviders, name)
}

// RemoveProvider removes a provider from the enabled list.
func (c *Config) RemoveProvider(name string) {
	out := c.EnabledProviders[:0]
	for _, p := range c.EnabledProviders {
		if p != name {
			out = append(out, p)
		}
	}
	c.EnabledProviders = out
}

// EnsureDeviceID returns a stable, random, pseudonymous device identifier.
// It deliberately does not derive identity from hostname, username, hardware,
// network interfaces, or any other machine fingerprint.
func (c *Config) EnsureDeviceID() (string, error) {
	if c.Inventory.DeviceID != "" {
		return c.Inventory.DeviceID, nil
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate device id: %w", err)
	}
	c.Inventory.DeviceID = "dev_" + hex.EncodeToString(raw[:])
	return c.Inventory.DeviceID, nil
}
