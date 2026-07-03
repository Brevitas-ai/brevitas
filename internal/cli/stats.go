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
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return err
	}

	a.say("Brevitas token savings\n")
	fmt.Fprintf(a.Out, "  Requests proxied     %d\n", s.Requests)
	fmt.Fprintf(a.Out, "  Requests optimized   %d\n", s.Optimized)
	fmt.Fprintf(a.Out, "  Tokens before        %d\n", s.TokensBefore)
	fmt.Fprintf(a.Out, "  Tokens after         %d\n", s.TokensAfter)
	fmt.Fprintf(a.Out, "  Tokens saved         %d (%.1f%%)\n", s.TokensSaved, s.SavedPct)
	if s.Requests == 0 {
		a.say("\nNo requests yet. Point a tool at the proxy or send one with curl.")
	}
	return nil
}
