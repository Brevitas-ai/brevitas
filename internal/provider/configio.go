package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Journal records every file Brevitas backs up so that Uninstall/repair can
// restore the exact original bytes. It is stored as JSON and guarded by a
// mutex for concurrent provider installs.
type Journal struct {
	path string
	mu   sync.Mutex

	Entries map[string]JournalEntry `json:"entries"`
}

// JournalEntry captures one managed file.
type JournalEntry struct {
	Provider   string    `json:"provider"`
	Original   string    `json:"original"`    // path of the live file
	Backup     string    `json:"backup"`      // path of the saved copy ("" if file was absent)
	Existed    bool      `json:"existed"`     // whether the file existed before Brevitas
	OrigSHA256 string    `json:"orig_sha256"` // checksum of the original bytes
	SavedAt    time.Time `json:"saved_at"`
}

// LoadJournal reads (or initializes) the rollback journal at path.
func LoadJournal(path string) (*Journal, error) {
	j := &Journal{path: path, Entries: map[string]JournalEntry{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return j, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read journal: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, j); err != nil {
			return nil, fmt.Errorf("parse journal: %w", err)
		}
		if j.Entries == nil {
			j.Entries = map[string]JournalEntry{}
		}
	}
	return j, nil
}

func (j *Journal) save() error {
	if err := os.MkdirAll(filepath.Dir(j.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	tmp := j.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, j.path)
}

// Backup records the current contents of file (which may not exist) under the
// given provider and returns the entry. Repeated calls for an already-tracked
// file are no-ops so that re-running install does not overwrite the pristine
// backup with an already-modified file.
func (j *Journal) Backup(providerName, file, backupDir string) (JournalEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if e, ok := j.Entries[file]; ok {
		return e, nil
	}

	entry := JournalEntry{
		Provider: providerName,
		Original: file,
		SavedAt:  time.Now().UTC(),
	}

	data, err := os.ReadFile(file)
	switch {
	case os.IsNotExist(err):
		entry.Existed = false
	case err != nil:
		return JournalEntry{}, fmt.Errorf("read %s for backup: %w", file, err)
	default:
		entry.Existed = true
		sum := sha256.Sum256(data)
		entry.OrigSHA256 = hex.EncodeToString(sum[:])

		if err := os.MkdirAll(backupDir, 0o755); err != nil {
			return JournalEntry{}, err
		}
		backup := filepath.Join(backupDir, fmt.Sprintf("%s-%s.bak", providerName, sanitize(file)))
		if err := os.WriteFile(backup, data, 0o600); err != nil {
			return JournalEntry{}, fmt.Errorf("write backup: %w", err)
		}
		entry.Backup = backup
	}

	j.Entries[file] = entry
	if err := j.save(); err != nil {
		return JournalEntry{}, err
	}
	return entry, nil
}

// Restore returns the tracked file to its original state and drops the entry.
// If the file did not exist originally, it is removed.
func (j *Journal) Restore(file string) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	entry, ok := j.Entries[file]
	if !ok {
		return nil // nothing tracked; leave the file alone
	}

	if !entry.Existed {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", file, err)
		}
	} else {
		data, err := os.ReadFile(entry.Backup)
		if err != nil {
			return fmt.Errorf("read backup %s: %w", entry.Backup, err)
		}
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file, data, 0o600); err != nil {
			return fmt.Errorf("restore %s: %w", file, err)
		}
		_ = os.Remove(entry.Backup)
	}

	delete(j.Entries, file)
	return j.save()
}

// RestoreProvider restores every file tracked for a provider.
func (j *Journal) RestoreProvider(providerName string) error {
	j.mu.Lock()
	files := make([]string, 0)
	for f, e := range j.Entries {
		if e.Provider == providerName {
			files = append(files, f)
		}
	}
	j.mu.Unlock()

	sort.Strings(files)
	for _, f := range files {
		if err := j.Restore(f); err != nil {
			return err
		}
	}
	return nil
}

func sanitize(p string) string {
	sum := sha256.Sum256([]byte(p))
	return hex.EncodeToString(sum[:8])
}
