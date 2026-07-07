package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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

		InputTokens      int64   `json:"input_tokens"`
		OutputTokens     int64   `json:"output_tokens"`
		CacheReadTokens  int64   `json:"cache_read_tokens"`
		CacheWriteTokens int64   `json:"cache_write_tokens"`
		CacheReadPct     float64 `json:"cache_read_pct"`
		CostSavedUSD     float64 `json:"cost_saved_usd"`
		PricedResponses  int64   `json:"priced_responses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return err
	}

	a.say("Brevitas savings\n")
	fmt.Fprintf(a.Out, "  Requests proxied       %d\n", s.Requests)
	a.say("\n  Caching (lossless — the default engine):")
	fmt.Fprintf(a.Out, "    Native prompt caching  %d  requests with provider cache breakpoints set\n", s.NativeCache)
	fmt.Fprintf(a.Out, "    Response-cache hits    %d  replies served with no upstream call\n", s.CacheHits)

	// The honest answer to "did caching save money", read off the real usage
	// block each provider returns — not an estimate.
	a.say("\n  Measured from real provider responses:")
	fmt.Fprintf(a.Out, "    Input served from cache  %s of %s input tokens (%.1f%%)\n",
		humanInt(s.CacheReadTokens), humanInt(s.InputTokens+s.CacheReadTokens+s.CacheWriteTokens), s.CacheReadPct)
	fmt.Fprintf(a.Out, "    Cache writes (one-time)  %s tokens\n", humanInt(s.CacheWriteTokens))
	if s.PricedResponses > 0 {
		fmt.Fprintf(a.Out, "    Real dollars saved       $%.4f  (across %d priced responses)\n",
			s.CostSavedUSD, s.PricedResponses)
	} else {
		a.say("    Real dollars saved       n/a  (no responses with a known model price yet)")
	}

	a.say("\n  Prompt compression (only when lossy compression is enabled):")
	fmt.Fprintf(a.Out, "    Requests compressed    %d\n", s.Optimized)
	fmt.Fprintf(a.Out, "    Tokens trimmed         %d of %d (%.1f%%)\n", s.TokensSaved, s.TokensBefore, s.SavedPct)

	switch {
	case s.Requests == 0:
		a.say("\nNo requests yet. Point a tool at the proxy or send one with curl.")
	case s.NativeCache > 0 && s.CacheReadTokens == 0:
		a.say("\nBreakpoints were set but nothing has been read back from cache yet, so")
		a.say("they've saved nothing so far. Cache reads land once the same prefix")
		a.say("repeats within the provider's cache window — send more turns to bank it.")
	case s.CacheReadTokens > 0 && s.PricedResponses == 0:
		a.say("\nCache reads are landing, but these models aren't in the price table, so")
		a.say("only the token counts are shown. The dollars are real once priced.")
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

// humanInt formats a token count with thousands separators, e.g. 1234567 ->
// "1,234,567", so large cache-read counts stay readable in the summary.
func humanInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := ""
	if strings.HasPrefix(s, "-") {
		neg, s = "-", s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return neg + s
}
