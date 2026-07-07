package proxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Brevitas-ai/brevitas/internal/optimizer"
)

// optimizerKeyID is a short, non-reversible identity for the Brevitas API key,
// used to namespace the response cache so answers never cross tenants. Empty in,
// empty out (single-tenant local default).
func optimizerKeyID(apiKey string) string {
	if apiKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:])[:16]
}

// handle is the main request handler. It reads the body, asks brevitas-systems
// to optimize it (failing open on any optimizer error so coding assistants
// keep working), then forwards to the correct upstream with retries and
// streaming support.
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := time.Now()

	// Health endpoint for the local tools and doctor.
	if r.URL.Path == "/__brevitas/health" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}
	// Token-savings stats endpoint (consumed by `brevitas stats`).
	if r.URL.Path == "/__brevitas/stats" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.stats.snapshot())
		return
	}

	rt := classify(r)
	if rt.Family == FamilyUnknown {
		s.writeError(w, http.StatusNotFound, "brevitas: no upstream mapping for %s", r.URL.Path)
		return
	}

	upstreamBase, ok := s.cfg.Upstreams[string(rt.Family)]
	if !ok || upstreamBase == "" {
		s.writeError(w, http.StatusBadGateway, "brevitas: no upstream configured for %s", rt.Family)
		return
	}
	s.stats.markRequest()

	body, err := io.ReadAll(io.LimitReader(r.Body, s.cfg.Proxy.MaxBodyBytes))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "brevitas: read request body: %v", err)
		return
	}

	meta := extractMeta(body)

	// Load the single Brevitas API key once per request from the OS keyring.
	apiKey, keyErr := s.apiKey(ctx)
	if keyErr != nil {
		s.log.Warn("no api key available", "err", keyErr)
	}

	// Optimize via brevitas-systems (fail-open). Only attempt when the body is
	// non-empty, valid JSON — GET/empty/non-JSON requests (e.g. /v1/models,
	// token counting, health) have nothing to optimize and would otherwise
	// error on json.RawMessage marshaling.
	outBody := body
	optHeaders := map[string]string{}
	if s.opt != nil && len(body) > 0 && json.Valid(body) {
		optReq := &optimizer.Request{
			Provider: string(rt.Family),
			Model:    meta.Model,
			Stream:   meta.Stream,
			Path:     r.URL.Path,
			Headers:  flattenHeaders(r.Header),
			Body:     json.RawMessage(body),
			KeyID:    optimizerKeyID(apiKey),
		}
		optCtx, cancel := context.WithTimeout(ctx, s.cfg.Optimizer.CallTimeout)
		resp, oerr := s.opt.Optimize(optCtx, optReq)
		cancel()
		switch {
		case oerr != nil:
			s.log.Warn("optimizer failed, forwarding original", "family", rt.Family, "err", oerr)
		case resp.CacheHit && len(resp.CachedResponse) > 0 && !meta.Stream:
			// Response cache hit: replay the stored answer and skip the upstream
			// call entirely (100% savings on this call). Cache never serves streams.
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Brevitas-Cache", "hit")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(resp.CachedResponse)
			s.stats.markCacheHit()
			s.log.Info("cache hit", "family", rt.Family, "model", meta.Model,
				"kind", resp.CacheKind, "dur_ms", time.Since(start).Milliseconds())
			return
		case resp.Bypass:
			s.log.Debug("optimizer bypassed request", "family", rt.Family)
		default:
			if len(resp.Body) > 0 {
				outBody = resp.Body
			}
			optHeaders = resp.Headers
			// Native prompt caching is the lossless engine's main savings
			// mechanism — the prompt is unchanged, so it never shows as a token
			// reduction; count it separately so `bvx stats` reflects it.
			if appliedHasNativeCache(resp.Applied) {
				s.stats.markNativeCache()
			}
			if sv := resp.Savings; sv != nil {
				s.stats.record(sv.TokensBefore, sv.TokensAfter)
				s.log.Info("optimized request",
					"family", rt.Family,
					"model", meta.Model,
					"tokens_before", sv.TokensBefore,
					"tokens_after", sv.TokensAfter,
					"saved_pct", sv.SavedPct,
					"method", sv.Method,
					"applied", resp.Applied,
				)
			} else {
				s.log.Debug("optimized request", "family", rt.Family, "applied", resp.Applied)
			}
		}
	}

	resp, err := s.forward(ctx, rt, upstreamBase, outBody, r, apiKey, optHeaders, meta.Stream)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, "brevitas: upstream request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	// Non-stream 200s are handed back to the sidecar to populate the response
	// cache (it applies its own cacheable() gate) and report usage. Streams and
	// errors are streamed straight through and never cached.
	if s.opt != nil && !meta.Stream && resp.StatusCode == http.StatusOK {
		s.streamAndRecord(w, resp, &optimizer.RecordRequest{
			Provider: string(rt.Family),
			Model:    meta.Model,
			KeyID:    optimizerKeyID(apiKey),
			Headers:  flattenHeaders(r.Header),
			Body:     json.RawMessage(body),
		})
	} else {
		w.Header().Set("X-Brevitas-Cache", "miss")
		s.streamResponse(w, resp)
	}
	s.log.Info("proxied",
		"family", rt.Family,
		"model", meta.Model,
		"status", resp.StatusCode,
		"stream", meta.Stream,
		"dur_ms", time.Since(start).Milliseconds(),
	)
}

// forward builds and executes the upstream request with retries.
func (s *Server) forward(
	ctx context.Context,
	rt route,
	upstreamBase string,
	body []byte,
	orig *http.Request,
	apiKey string,
	optHeaders map[string]string,
	streaming bool,
) (*http.Response, error) {
	target, err := upstreamURL(upstreamBase, rt.Path)
	if err != nil {
		return nil, fmt.Errorf("build upstream url: %w", err)
	}

	maxAttempts := s.cfg.Proxy.MaxRetries + 1
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, orig.Method, target.String(), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		copyRequestHeaders(req.Header, orig.Header)
		for k, v := range optHeaders {
			req.Header.Set(k, v)
		}
		// Default is passthrough: the tool's own provider credentials (copied
		// above) reach the provider untouched. Only a Brevitas *gateway*
		// upstream needs the single stored key injected.
		if s.cfg.Proxy.UpstreamAuth == "inject" {
			applyGatewayAuth(req, rt.Family, apiKey)
		}
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(len(body))

		resp, err := s.transport.RoundTrip(req)
		if err != nil {
			lastErr = err
			if !s.retryable(ctx, attempt, maxAttempts) {
				return nil, err
			}
			s.backoff(ctx, attempt)
			continue
		}

		if shouldRetryStatus(resp.StatusCode) && attempt < maxAttempts {
			lastErr = fmt.Errorf("upstream status %d", resp.StatusCode)
			resp.Body.Close()
			s.backoff(ctx, attempt)
			continue
		}
		return resp, nil
	}

	if lastErr == nil {
		lastErr = errors.New("exhausted retries")
	}
	return nil, lastErr
}

func (s *Server) retryable(ctx context.Context, attempt, maxAttempts int) bool {
	return ctx.Err() == nil && attempt < maxAttempts
}

func (s *Server) backoff(ctx context.Context, attempt int) {
	d := time.Duration(attempt) * 200 * time.Millisecond
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func shouldRetryStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// requestMeta holds fields extracted from the JSON body for routing/telemetry.
type requestMeta struct {
	Model  string
	Stream bool
}

func extractMeta(body []byte) requestMeta {
	var m struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	_ = json.Unmarshal(body, &m) // best effort
	return requestMeta{Model: m.Model, Stream: m.Stream}
}

// appliedHasNativeCache reports whether brevitas-systems inserted provider
// native prompt-cache breakpoints on this request (applied pass "native_cache").
func appliedHasNativeCache(applied []string) bool {
	for _, a := range applied {
		if a == "native_cache" {
			return true
		}
	}
	return false
}

func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k := range h {
		out[k] = h.Get(k)
	}
	return out
}
