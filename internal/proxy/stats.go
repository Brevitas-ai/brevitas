package proxy

import (
	"sync/atomic"
	"time"
)

// Stats accumulates token-savings metrics across requests. It is safe for
// concurrent use and cheap enough to update on every request.
type Stats struct {
	Requests     atomic.Int64
	Optimized    atomic.Int64
	TokensBefore atomic.Int64
	TokensAfter  atomic.Int64
	CacheHits    atomic.Int64
	NativeCache  atomic.Int64

	// Real usage measured off each provider response — the honest source for
	// "did native prompt caching actually save money". cacheRead/cacheWrite are
	// the provider's own reported token counts, not an estimate.
	InputTokens      atomic.Int64
	OutputTokens     atomic.Int64
	CacheReadTokens  atomic.Int64
	CacheWriteTokens atomic.Int64
	// CostSavedMicros is cumulative dollars saved by caching, in micro-USD
	// (1e-6 USD), summed only over responses whose model we can price.
	CostSavedMicros atomic.Int64
	// PricedResponses counts responses that carried usage AND a known model
	// price, so the dollar figure can be shown with its coverage.
	PricedResponses atomic.Int64

	startedUnix int64
}

func newStats() *Stats {
	s := &Stats{}
	s.startedUnix = time.Now().Unix()
	return s
}

// markRequest counts one proxied API request (whether or not it was optimized).
func (s *Stats) markRequest() { s.Requests.Add(1) }

// markCacheHit counts one request served from the response cache (no upstream call).
func (s *Stats) markCacheHit() { s.CacheHits.Add(1) }

// markNativeCache counts one request where brevitas-systems inserted provider
// native prompt-cache breakpoints. This is the lossless engine's main savings
// mechanism: the prompt is unchanged (before == after), but repeated context is
// billed by the provider at the cheaper cached-input rate.
func (s *Stats) markNativeCache() { s.NativeCache.Add(1) }

// record folds one request's savings into the totals.
func (s *Stats) record(before, after int) {
	if before > 0 && after >= 0 && after < before {
		s.Optimized.Add(1)
	}
	s.TokensBefore.Add(int64(before))
	s.TokensAfter.Add(int64(after))
}

// recordUsage folds one response's real token usage (and the dollars it saved
// via caching) into the totals. A zero usage is a no-op.
func (s *Stats) recordUsage(family Family, model string, u usage) {
	if u.empty() {
		return
	}
	s.InputTokens.Add(u.inputTokens)
	s.OutputTokens.Add(u.outputTokens)
	s.CacheReadTokens.Add(u.cacheRead)
	s.CacheWriteTokens.Add(u.cacheWrite)
	if micros, known := savedMicroUSD(family, model, u); known {
		s.CostSavedMicros.Add(micros)
		s.PricedResponses.Add(1)
	}
}

// Snapshot is a serializable view of the counters.
type Snapshot struct {
	Requests     int64   `json:"requests"`
	Optimized    int64   `json:"optimized_requests"`
	TokensBefore int64   `json:"tokens_before"`
	TokensAfter  int64   `json:"tokens_after"`
	TokensSaved  int64   `json:"tokens_saved"`
	SavedPct     float64 `json:"saved_pct"`
	CacheHits    int64   `json:"cache_hits"`
	NativeCache  int64   `json:"native_cache"`

	// Real usage measured off provider responses.
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	CacheReadPct     float64 `json:"cache_read_pct"`
	CostSavedUSD     float64 `json:"cost_saved_usd"`
	PricedResponses  int64   `json:"priced_responses"`

	SinceUnix int64 `json:"since_unix"`
}

func (s *Stats) snapshot() Snapshot {
	before := s.TokensBefore.Load()
	after := s.TokensAfter.Load()
	saved := before - after
	var pct float64
	if before > 0 {
		pct = float64(saved) / float64(before) * 100
	}
	cacheRead := s.CacheReadTokens.Load()
	cacheWrite := s.CacheWriteTokens.Load()
	input := s.InputTokens.Load()
	totalInput := input + cacheRead + cacheWrite
	var cacheReadPct float64
	if totalInput > 0 {
		cacheReadPct = float64(cacheRead) / float64(totalInput) * 100
	}
	return Snapshot{
		Requests:         s.Requests.Load(),
		Optimized:        s.Optimized.Load(),
		TokensBefore:     before,
		TokensAfter:      after,
		TokensSaved:      saved,
		SavedPct:         pct,
		CacheHits:        s.CacheHits.Load(),
		NativeCache:      s.NativeCache.Load(),
		InputTokens:      input,
		OutputTokens:     s.OutputTokens.Load(),
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: cacheWrite,
		CacheReadPct:     cacheReadPct,
		CostSavedUSD:     float64(s.CostSavedMicros.Load()) / 1e6,
		PricedResponses:  s.PricedResponses.Load(),
		SinceUnix:        s.startedUnix,
	}
}
