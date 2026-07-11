package cli

import (
	"context"
	"testing"

	"github.com/Brevitas-ai/brevitas/internal/service"
)

type refreshManager struct{ installs, starts int }

func (m *refreshManager) Install(context.Context) error { m.installs++; return nil }
func (m *refreshManager) Start(context.Context) error   { m.starts++; return nil }
func (*refreshManager) Uninstall(context.Context) error { return nil }
func (*refreshManager) Stop(context.Context) error      { return nil }
func (*refreshManager) Restart(context.Context) error   { return nil }
func (*refreshManager) Status(context.Context) (service.State, error) {
	return service.StateRunning, nil
}
func (*refreshManager) Backend() string { return "test" }

func TestEnsureStartedRefreshesExistingService(t *testing.T) {
	mgr := &refreshManager{}
	if err := (&App{}).ensureStarted(context.Background(), mgr); err != nil {
		t.Fatal(err)
	}
	if mgr.installs != 1 || mgr.starts != 1 {
		t.Fatalf("install=%d start=%d, want 1 each", mgr.installs, mgr.starts)
	}
}
