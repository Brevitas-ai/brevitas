package service

import (
	"testing"

	"github.com/Brevitas-ai/brevitas/internal/config"
)

func TestProxyAndOptimizerSpecsDiffer(t *testing.T) {
	dirs := config.Dirs{Config: t.TempDir(), Data: t.TempDir(), Logs: t.TempDir(), Cache: t.TempDir()}

	proxy, err := ProxySpec(dirs)
	if err != nil {
		t.Fatalf("proxy spec: %v", err)
	}
	opt, err := OptimizerSpec(dirs)
	if err != nil {
		t.Fatalf("optimizer spec: %v", err)
	}

	if proxy.Label == opt.Label {
		t.Errorf("labels must differ, both %q", proxy.Label)
	}
	if proxy.Name == opt.Name {
		t.Errorf("names must differ, both %q", proxy.Name)
	}
	if len(proxy.Args) == 0 || proxy.Args[0] != "serve" {
		t.Errorf("proxy args = %v, want [serve]", proxy.Args)
	}
	if len(opt.Args) == 0 || opt.Args[0] != "optimizer" {
		t.Errorf("optimizer args = %v, want [optimizer]", opt.Args)
	}
	if proxy.Executable == "" || opt.Executable == "" {
		t.Error("executable path not resolved")
	}

	// NewManager must produce a usable manager on every platform.
	if NewManager(proxy).Backend() == "" {
		t.Error("empty backend name")
	}
}
