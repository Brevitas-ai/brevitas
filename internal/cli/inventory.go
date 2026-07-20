package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/Brevitas-ai/brevitas/internal/cloud"
	"github.com/Brevitas-ai/brevitas/internal/config"
)

const (
	defaultHeartbeatInterval = 15 * time.Minute
	heartbeatCheckInterval   = time.Minute
)

func inventoryID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return prefix + hex.EncodeToString(h.Sum(nil)[:16])
}

func installationUUID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	b := h.Sum(nil)[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// safeInventoryLabel removes path semantics and control characters. AgentMap
// inventory intentionally sends only the final repository directory name.
func safeInventoryLabel(raw string, limit int) string {
	raw = strings.TrimSuffix(strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/"), "/"), ".git")
	if slash := strings.LastIndexByte(raw, '/'); slash >= 0 {
		raw = raw[slash+1:]
	}
	raw = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return -1
		}
		return r
	}, raw)
	raw = strings.TrimSpace(raw)
	if limit > 0 && len(raw) > limit {
		end := 0
		for index := range raw {
			if index > limit {
				break
			}
			end = index
		}
		if end == 0 {
			return ""
		}
		raw = raw[:end]
	}
	return raw
}

func inventoryEnvironment(raw string) string {
	value := safeInventoryLabel(raw, 64)
	if value == "" {
		return "local"
	}
	return value
}

func (a *App) saveInventoryConfig() error {
	dirs := a.Dirs
	if dirs.Config == "" {
		dirs = config.ResolveDirs()
	}
	path := dirs.ConfigFile()
	if _, err := os.Stat(path); err == nil {
		latest, loadErr := config.LoadFrom(path)
		if loadErr != nil {
			return loadErr
		}
		latest.Inventory = a.Cfg.Inventory
		return latest.SaveTo(path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return a.Cfg.SaveTo(path)
}

func (a *App) installation(deviceID, repoLabel, environment string) *config.InstallationConfig {
	repositoryID := inventoryID("repo_", deviceID, repoLabel)
	installationID := installationUUID(deviceID, repositoryID, environment)
	for i := range a.Cfg.Inventory.Installations {
		item := &a.Cfg.Inventory.Installations[i]
		if item.ID == installationID || (item.RepositoryID == repositoryID && item.Environment == environment) {
			item.ID = installationID
			return item
		}
	}
	a.Cfg.Inventory.Installations = append(a.Cfg.Inventory.Installations, config.InstallationConfig{
		ID: installationID, RepositoryID: repositoryID,
		RepositoryLabel: repoLabel, Environment: environment,
	})
	return &a.Cfg.Inventory.Installations[len(a.Cfg.Inventory.Installations)-1]
}

func registrationFor(deviceID string, item config.InstallationConfig) cloud.InstallationRegistration {
	return cloud.InstallationRegistration{
		InstallationID: item.ID,
		Device:         cloud.Device(deviceID),
		Repository: cloud.RepositoryMetadata{
			ID: item.RepositoryID, Label: item.RepositoryLabel,
		},
		Environment: item.Environment,
		Client:      cloud.Client(),
	}
}

// registerCodebaseInstallation records the safe local identity before making
// the network call so a later service cycle can retry transient failures. It
// never persists the organization service key.
func (a *App) registerCodebaseInstallation(ctx context.Context, apiKey, repositoryPath, environment string) error {
	deviceID, err := a.ensureDeviceIdentity()
	if err != nil {
		return err
	}
	repoLabel := safeInventoryLabel(filepath.Base(repositoryPath), 128)
	if repoLabel == "" {
		return errors.New("repository has no safe label")
	}
	environment = inventoryEnvironment(environment)
	item := a.installation(deviceID, repoLabel, environment)
	if err := a.saveInventoryConfig(); err != nil {
		return err
	}

	response, err := cloud.RegisterInstallation(ctx, apiKey, registrationFor(deviceID, *item))
	if errors.Is(err, cloud.ErrInstallationUnsupported) {
		// Rolling-upgrade compatibility with the original dashboard contract.
		return cloud.RegisterRepository(ctx, apiKey, repoLabel)
	}
	if err != nil {
		return err
	}
	if response != nil {
		if response.InstallationID != "" {
			item.ID = response.InstallationID
		}
		item.HeartbeatIntervalSecs = response.HeartbeatIntervalSecs
	}
	item.Registered = true
	item.LastHeartbeatAt = time.Now().UTC()
	return a.saveInventoryConfig()
}

func heartbeatDue(item config.InstallationConfig, now time.Time) bool {
	interval := time.Duration(item.HeartbeatIntervalSecs) * time.Second
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	return item.LastHeartbeatAt.IsZero() || !now.Before(item.LastHeartbeatAt.Add(interval))
}

// inventoryCycle registers pending installations and heartbeats active ones.
// Errors are isolated per installation so one stale record never stops the
// service or another repository's heartbeat.
func (a *App) inventoryCycle(ctx context.Context, now time.Time, logger *slog.Logger) {
	if len(a.Cfg.Inventory.Installations) == 0 {
		return
	}
	apiKey, err := a.apiKeyFunc()(ctx)
	if err != nil || apiKey == "" {
		logger.Debug("inventory heartbeat skipped: organization key unavailable")
		return
	}
	deviceID := a.Cfg.Inventory.DeviceID
	if deviceID == "" {
		return
	}
	changed := false
	for i := range a.Cfg.Inventory.Installations {
		item := &a.Cfg.Inventory.Installations[i]
		if !item.Registered {
			response, registerErr := cloud.RegisterInstallation(ctx, apiKey, registrationFor(deviceID, *item))
			if registerErr != nil {
				logger.Debug("inventory registration unavailable", "installation_id", item.ID, "err", registerErr)
				continue
			}
			if response != nil {
				if response.InstallationID != "" {
					item.ID = response.InstallationID
				}
				item.HeartbeatIntervalSecs = response.HeartbeatIntervalSecs
			}
			item.Registered = true
			item.LastHeartbeatAt = now.UTC()
			changed = true
			continue
		}
		if !heartbeatDue(*item, now) {
			continue
		}
		response, heartbeatErr := cloud.HeartbeatInstallation(ctx, apiKey, item.ID, cloud.InstallationHeartbeat{
			Device: cloud.Device(deviceID), Environment: item.Environment, Client: cloud.Client(),
		})
		if heartbeatErr != nil {
			logger.Debug("inventory heartbeat unavailable", "installation_id", item.ID, "err", heartbeatErr)
			continue
		}
		if response != nil && response.HeartbeatIntervalSecs > 0 {
			item.HeartbeatIntervalSecs = response.HeartbeatIntervalSecs
		}
		item.LastHeartbeatAt = now.UTC()
		changed = true
	}
	if changed {
		if err := a.saveInventoryConfig(); err != nil {
			logger.Warn("inventory state could not be saved", "err", err)
		}
	}
}

func (a *App) runInventoryHeartbeats(ctx context.Context, logger *slog.Logger) {
	a.inventoryCycle(ctx, time.Now(), logger)
	ticker := time.NewTicker(heartbeatCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			a.inventoryCycle(ctx, now, logger)
		}
	}
}
