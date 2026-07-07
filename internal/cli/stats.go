package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// cmdStats prints cumulative token-savings metrics from the running proxy.
func (a *App) cmdStats(ctx context.Context, _ []string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.Cfg.ProxyURL()+"/__brevitas/stats", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("proxy not reachable (is it running?): %w", err)
	}
	defer resp.Body.Close()

	var s struct {
		Requests     int64   `json:"requests"`
		Optimized    int64   `json:"optimized_requests"`
		TokensBefore int64   `json:"tokens_before"`
		TokensAfter  int64   `json:"tokens_after"`
		TokensSaved  int64   `json:"tokens_saved"`
		SavedPct     float64 `json:"saved_pct"`
		CacheHits    int64   `json:"cache_hits"`
		NativeCache  int64   `json:"native_cache"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return err
	}

	a.say("Brevitas savings\n")
	fmt.Fprintf(a.Out, "  Requests proxied       %d\n", s.Requests)
	a.say("\n  Caching (lossless — the default engine):")
	fmt.Fprintf(a.Out, "    Native prompt caching  %d  requests with provider cache breakpoints set\n", s.NativeCache)
	fmt.Fprintf(a.Out, "    Response-cache hits    %d  replies served with no upstream call\n", s.CacheHits)
	a.say("\n  Prompt compression (only when lossy compression is enabled):")
	fmt.Fprintf(a.Out, "    Requests compressed    %d\n", s.Optimized)
	fmt.Fprintf(a.Out, "    Tokens trimmed         %d of %d (%.1f%%)\n", s.TokensSaved, s.TokensBefore, s.SavedPct)

	switch {
	case s.Requests == 0:
		a.say("\nNo requests yet. Point a tool at the proxy or send one with curl.")
	case s.TokensSaved == 0 && (s.NativeCache > 0 || s.CacheHits > 0):
		a.say("\nThe lossless engine saves via provider-side prompt caching: the prompt")
		a.say("is unchanged, so cached input tokens are billed cheaper by the provider")
		a.say("rather than showing up as a raw token reduction here.")
	case s.TokensSaved == 0:
		a.say("\nNo savings recorded yet. The default engine is lossless (it caches rather")
		a.say("than shrinks prompts); enable lossy compression to trim tokens directly.")
	}
	return nil
}
