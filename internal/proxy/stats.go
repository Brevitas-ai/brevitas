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
	startedUnix  int64
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

// record folds one request's savings into the totals.
func (s *Stats) record(before, after int) {
	if before > 0 && after >= 0 && after < before {
		s.Optimized.Add(1)
	}
	s.TokensBefore.Add(int64(before))
	s.TokensAfter.Add(int64(after))
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
	SinceUnix    int64   `json:"since_unix"`
}

func (s *Stats) snapshot() Snapshot {
	before := s.TokensBefore.Load()
	after := s.TokensAfter.Load()
	saved := before - after
	var pct float64
	if before > 0 {
		pct = float64(saved) / float64(before) * 100
	}
	return Snapshot{
		Requests:     s.Requests.Load(),
		Optimized:    s.Optimized.Load(),
		TokensBefore: before,
		TokensAfter:  after,
		TokensSaved:  saved,
		SavedPct:     pct,
		CacheHits:    s.CacheHits.Load(),
		SinceUnix:    s.startedUnix,
	}
}
