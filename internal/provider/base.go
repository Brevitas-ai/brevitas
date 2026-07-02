package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Base provides shared behavior for providers that manage documented
// configuration files. Concrete providers embed it, supply metadata, and
// implement Detect/Install/Uninstall/Validate using the helpers below.
type Base struct {
	Env *Env

	name    string
	display string
	support Support
	// reason explains an unsupported classification (user-facing).
	reason string
}

// NewBase constructs the embedded base with static metadata.
func NewBase(env *Env, name, display string, support Support, reason string) Base {
	return Base{Env: env, name: name, display: display, support: support, reason: reason}
}

func (b *Base) Name() string        { return b.name }
func (b *Base) DisplayName() string { return b.display }
func (b *Base) Support() Support    { return b.support }
func (b *Base) Reason() string      { return b.reason }

// APIKeyValue fetches the Brevitas API key that the tool will send to the
// local proxy. Providers that store a key in their config call this.
func (b *Base) APIKeyValue(ctx context.Context) (string, error) {
	if b.Env == nil || b.Env.APIKey == nil {
		return "", fmt.Errorf("no API key source configured")
	}
	return b.Env.APIKey(ctx)
}

// ProxyURL returns the loopback URL tools should target.
func (b *Base) ProxyURL() string {
	if b.Env != nil && b.Env.ProxyURL != "" {
		return b.Env.ProxyURL
	}
	if b.Env != nil && b.Env.Config != nil {
		return b.Env.Config.ProxyURL()
	}
	return "http://127.0.0.1:8080"
}

// OpenAIBaseURL returns the base URL OpenAI-compatible clients should use.
// These clients append paths like "/chat/completions", so the "/v1" segment
// is included here (the proxy routes on the "/v1/" prefix).
func (b *Base) OpenAIBaseURL() string { return b.ProxyURL() + "/v1" }

// journal opens the rollback journal.
func (b *Base) journal() (*Journal, error) {
	return LoadJournal(b.Env.Dirs.JournalFile())
}

// EditJSON safely edits (or creates) a JSON config file. It backs the file up
// first, invokes mutate on the decoded root object, then writes the result
// atomically. When the file does not exist, mutate receives an empty map.
//
// This is the workhorse most providers use — it guarantees a rollback point
// and never destroys unrelated keys.
func (b *Base) EditJSON(file string, mutate func(root map[string]any) error) error {
	j, err := b.journal()
	if err != nil {
		return err
	}
	if _, err := j.Backup(b.name, file, b.Env.Dirs.BackupDir()); err != nil {
		return err
	}

	root := map[string]any{}
	if data, err := os.ReadFile(file); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("%s: existing config is not valid JSON: %w", file, err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", file, err)
	}

	if err := mutate(root); err != nil {
		return err
	}

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", file, err)
	}
	return atomicWrite(file, out)
}

// WriteManagedFile writes an entire file that Brevitas owns (used for tools
// whose config is a standalone snippet). The prior content is journaled.
func (b *Base) WriteManagedFile(file string, content []byte) error {
	j, err := b.journal()
	if err != nil {
		return err
	}
	if _, err := j.Backup(b.name, file, b.Env.Dirs.BackupDir()); err != nil {
		return err
	}
	return atomicWrite(file, content)
}

// Managed-block markers delimit the region Brevitas owns inside an otherwise
// user-managed text config file (TOML, YAML, dotenv, INI...).
const (
	blockStart = "# >>> brevitas (managed) >>>"
	blockEnd   = "# <<< brevitas (managed) <<<"
)

// EditManagedBlock inserts or replaces a Brevitas-owned block inside a
// line-based config file, preserving all surrounding user content. The file is
// journaled first so Uninstall restores the exact original bytes. This is used
// for TOML/YAML/dotenv tools where a full structural rewrite would risk losing
// comments or formatting.
func (b *Base) EditManagedBlock(file, blockBody string) error {
	return b.EditManagedBlockAt(file, blockBody, false)
}

// EditManagedBlockAt is like EditManagedBlock but places a freshly-created
// block at the top of the file when atTop is true. This is required for TOML,
// where bare top-level keys must precede any table headers.
func (b *Base) EditManagedBlockAt(file, blockBody string, atTop bool) error {
	j, err := b.journal()
	if err != nil {
		return err
	}
	if _, err := j.Backup(b.name, file, b.Env.Dirs.BackupDir()); err != nil {
		return err
	}

	existing := ""
	if data, err := os.ReadFile(file); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", file, err)
	}

	block := blockStart + "\n" + strings.TrimRight(blockBody, "\n") + "\n" + blockEnd

	var next string
	if s := strings.Index(existing, blockStart); s >= 0 {
		if e := strings.Index(existing, blockEnd); e > s {
			// Replace the existing block in place, preserving its position.
			next = existing[:s] + block + existing[e+len(blockEnd):]
		} else {
			next = ensureTrailingNewline(existing) + block + "\n"
		}
	} else if atTop {
		next = block + "\n\n" + existing
	} else {
		next = ensureTrailingNewline(existing) + block + "\n"
	}

	return atomicWrite(file, []byte(next))
}

func ensureTrailingNewline(s string) string {
	if s == "" || strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

// Restore rolls back every file this provider modified.
func (b *Base) Restore() error {
	j, err := b.journal()
	if err != nil {
		return err
	}
	return j.RestoreProvider(b.name)
}

// ReadJSONField loads a JSON file and returns the value at a top-level key.
func ReadJSONField(file, key string) (any, bool) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, false
	}
	root := map[string]any{}
	if json.Unmarshal(data, &root) != nil {
		return nil, false
	}
	v, ok := root[key]
	return v, ok
}

func atomicWrite(file string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp := file + ".brevitas.tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// UnsupportedStatus is a helper returning a Status for tools that cannot be
// proxied, filling in detection state and the stored reason.
func (b *Base) UnsupportedStatus(detected bool) Status {
	st := StateNotDetected
	if detected {
		st = StateUnsupported
	}
	return Status{
		Name:     b.name,
		Support:  SupportUnsupported,
		State:    st,
		Detected: detected,
		Reason:   b.reason,
	}
}
