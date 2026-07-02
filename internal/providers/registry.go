// Package providers assembles the registry of every supported AI-tool
// integration. Each provider lives in its own subpackage and is registered
// here; nothing else in the codebase needs to know the concrete list.
package providers

import (
	"context"
	"sort"
	"sync"

	"github.com/brevitas-systems/brevitas/internal/provider"

	"github.com/brevitas-systems/brevitas/internal/providers/aider"
	"github.com/brevitas-systems/brevitas/internal/providers/anythingllm"
	"github.com/brevitas-systems/brevitas/internal/providers/claude"
	"github.com/brevitas-systems/brevitas/internal/providers/cline"
	"github.com/brevitas-systems/brevitas/internal/providers/codex"
	"github.com/brevitas-systems/brevitas/internal/providers/continueprov"
	"github.com/brevitas-systems/brevitas/internal/providers/copilot"
	"github.com/brevitas-systems/brevitas/internal/providers/cursor"
	"github.com/brevitas-systems/brevitas/internal/providers/geminicli"
	"github.com/brevitas-systems/brevitas/internal/providers/goose"
	"github.com/brevitas-systems/brevitas/internal/providers/jan"
	"github.com/brevitas-systems/brevitas/internal/providers/lmstudio"
	"github.com/brevitas-systems/brevitas/internal/providers/ollama"
	"github.com/brevitas-systems/brevitas/internal/providers/opencode"
	"github.com/brevitas-systems/brevitas/internal/providers/openwebui"
	"github.com/brevitas-systems/brevitas/internal/providers/vscodeopenai"
	"github.com/brevitas-systems/brevitas/internal/providers/windsurf"
)

// factories is the authoritative, ordered list of provider constructors. To
// add a new tool, implement a subpackage and append its New here.
var factories = []provider.Factory{
	claude.New,
	codex.New,
	cursor.New,
	continueprov.New,
	cline.New,
	aider.New,
	goose.New,
	opencode.New,
	geminicli.New,
	windsurf.New,
	vscodeopenai.New,
	lmstudio.New,
	openwebui.New,
	anythingllm.New,
	jan.New,
	ollama.New,
	copilot.New,
}

// Registry is a materialized set of providers bound to an Env.
type Registry struct {
	env       *provider.Env
	providers []provider.Provider
}

// New builds a Registry, constructing every provider with the injected Env.
func New(env *provider.Env) *Registry {
	ps := make([]provider.Provider, 0, len(factories))
	for _, f := range factories {
		ps = append(ps, f(env))
	}
	return &Registry{env: env, providers: ps}
}

// All returns every registered provider.
func (r *Registry) All() []provider.Provider { return r.providers }

// Get returns the provider with the given name, or nil.
func (r *Registry) Get(name string) provider.Provider {
	for _, p := range r.providers {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// Detected returns providers detected on this host, in parallel for speed.
func (r *Registry) Detected(ctx context.Context) []provider.Provider {
	type result struct {
		p        provider.Provider
		detected bool
	}
	results := make([]result, len(r.providers))
	var wg sync.WaitGroup
	for i, p := range r.providers {
		wg.Add(1)
		go func(i int, p provider.Provider) {
			defer wg.Done()
			results[i] = result{p: p, detected: p.Detect(ctx)}
		}(i, p)
	}
	wg.Wait()

	var out []provider.Provider
	for _, res := range results {
		if res.detected {
			out = append(out, res.p)
		}
	}
	return out
}

// Statuses returns a Status for every provider, sorted by name.
func (r *Registry) Statuses(ctx context.Context) []provider.Status {
	out := make([]provider.Status, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p.Status(ctx))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names returns every registered provider name in registration order.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p.Name())
	}
	return out
}
