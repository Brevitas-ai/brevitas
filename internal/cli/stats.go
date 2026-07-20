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
	a.page("Savings dashboard", "Local, content-free efficiency metrics from the BVX proxy.")
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.Cfg.ProxyURL()+"/__brevitas/stats", nil)
	if err != nil {
		return err
	}
	loading := a.startLoading("Loading savings metrics…")
	defer loading.Stop()
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

		InputTokens               int64   `json:"input_tokens"`
		OutputTokens              int64   `json:"output_tokens"`
		CacheReadTokens           int64   `json:"cache_read_tokens"`
		CacheWriteTokens          int64   `json:"cache_write_tokens"`
		CacheReadPct              float64 `json:"cache_read_pct"`
		AttributedCacheReadTokens int64   `json:"attributed_cache_read_tokens"`
		ClientCachedReadTokens    int64   `json:"client_cached_read_tokens"`
		CostSavedUSD              float64 `json:"cost_saved_usd"`
		PricedResponses           int64   `json:"priced_responses"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return err
	}
	loading.Stop()

	a.section("Traffic")
	a.metric("Requests proxied", humanInt(s.Requests), ansiCyan)
	a.section("Lossless caching")
	a.metric("Native prompt caching", humanInt(s.NativeCache)+" requests", ansiBlue)
	a.metric("Response-cache hits", humanInt(s.CacheHits)+" replies", ansiGreen)

	// Provider-side caching measured off real response usage. This is ALL cache
	// activity through the proxy — some of it (a client's own cache_control, or
	// OpenAI's automatic caching) happens with or without Brevitas.
	a.section("Provider cache activity")
	a.metric("Input served from cache", fmt.Sprintf("%s / %s tokens  ·  %.1f%%",
		humanInt(s.CacheReadTokens), humanInt(s.InputTokens+s.CacheReadTokens+s.CacheWriteTokens), s.CacheReadPct), ansiCyan)
	a.metric("One-time cache writes", humanInt(s.CacheWriteTokens)+" tokens", ansiBlue)

	// Only the share Brevitas actually caused becomes a dollar figure.
	a.section("Verified Brevitas impact")
	a.metric("Brevitas-driven reads", humanInt(s.AttributedCacheReadTokens)+" tokens", ansiGreen)
	a.metric("Client's own caching", humanInt(s.ClientCachedReadTokens)+" tokens", ansiGray)
	if s.PricedResponses > 0 {
		a.metric("Dollars saved", fmt.Sprintf("$%.4f  ·  %d priced responses", s.CostSavedUSD, s.PricedResponses), ansiGreen)
	} else {
		a.metric("Dollars saved", "$0.0000", ansiGray)
	}

	a.section("Prompt compression")
	a.metric("Requests compressed", humanInt(s.Optimized), ansiMagenta)
	a.metric("Tokens trimmed", fmt.Sprintf("%s / %s  ·  %.1f%%", humanInt(s.TokensSaved), humanInt(s.TokensBefore), s.SavedPct), ansiMagenta)

	switch {
	case s.Requests == 0:
		a.section("Insight")
		a.note("No requests yet. Point a tool at the proxy or send one with curl.")
	case s.ClientCachedReadTokens > 0 && s.AttributedCacheReadTokens == 0:
		a.section("Insight")
		a.note("These reads came from the client's own cache controls, so they are not credited to Brevitas.")
		a.note("Brevitas earns credit for cache breakpoints it inserts and response-cache hits it serves.")
	case s.NativeCache > 0 && s.CacheReadTokens == 0:
		a.section("Insight")
		a.note("Breakpoints are ready, but no prefix has repeated inside the provider cache window yet.")
	case s.AttributedCacheReadTokens > 0 && s.PricedResponses == 0:
		a.section("Insight")
		a.note("Brevitas-driven reads are landing, but these models are not in the price table yet.")
	case s.TokensSaved == 0:
		a.section("Insight")
		a.note("The default engine is lossless caching. Enable lossy compression to trim prompts directly.")
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
