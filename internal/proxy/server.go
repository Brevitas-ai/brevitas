// Package proxy implements the local HTTP proxy that AI coding tools are
// pointed at. It optimizes each request via brevitas-systems and forwards it
// to the upstream provider, supporting OpenAI-, Anthropic-, and
// Google-compatible APIs with streaming/SSE, retries, timeouts, large
// payloads, and connection pooling.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/Brevitas-ai/brevitas/internal/cloud"
	"github.com/Brevitas-ai/brevitas/internal/config"
	"github.com/Brevitas-ai/brevitas/internal/optimizer"
)

// APIKeyFunc returns the Brevitas API key used for upstream auth.
type APIKeyFunc func(ctx context.Context) (string, error)
type UsageReportFunc func(ctx context.Context, apiKey string, report cloud.UsageReport) error

// Server is the local optimization proxy.
type Server struct {
	cfg       *config.Config
	opt       optimizer.Client
	apiKey    APIKeyFunc
	log       *slog.Logger
	transport *http.Transport
	httpSrv   *http.Server
	stats     *Stats
	report    UsageReportFunc
}

// Options bundles the injected dependencies for a Server.
type Options struct {
	Config      *config.Config
	Optimizer   optimizer.Client
	APIKey      APIKeyFunc
	Logger      *slog.Logger
	ReportUsage UsageReportFunc
}

// New builds a proxy Server. All dependencies are injected for testability.
func New(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.APIKey == nil {
		opts.APIKey = func(context.Context) (string, error) { return "", nil }
	}
	if opts.ReportUsage == nil {
		opts.ReportUsage = cloud.ReportUsage
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   64,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	s := &Server{
		cfg:       opts.Config,
		opt:       opts.Optimizer,
		apiKey:    opts.APIKey,
		log:       opts.Logger,
		transport: transport,
		stats:     newStats(),
		report:    opts.ReportUsage,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)

	s.httpSrv = &http.Server{
		Addr:         opts.Config.Addr(),
		Handler:      mux,
		ReadTimeout:  opts.Config.Proxy.ReadTimeout,
		WriteTimeout: opts.Config.Proxy.WriteTimeout, // 0 => streaming friendly
		// No global handler timeout: streaming responses can be long-lived;
		// per-request bounds come from the client context and RequestTimeout.
	}
	return s
}

// Handler returns the proxy's HTTP handler. Exposed primarily so tests can
// drive the proxy through httptest without binding a real port.
func (s *Server) Handler() http.Handler { return s.httpSrv.Handler }

// ListenAndServe blocks serving requests until the context is cancelled or an
// error occurs. It performs a graceful shutdown on cancellation.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.httpSrv.Addr)
	if err != nil {
		return fmt.Errorf("bind %s: %w", s.httpSrv.Addr, err)
	}

	errCh := make(chan error, 1)
	go func() {
		s.log.Info("proxy listening", "addr", s.httpSrv.Addr)
		err := s.httpSrv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s.log.Info("proxy shutting down")
		_ = s.httpSrv.Shutdown(shutdownCtx)
		return <-errCh
	case err := <-errCh:
		return err
	}
}

// streamResponse copies the upstream response to the client, flushing so that
// SSE and chunked streaming reach the tool with minimal latency. When sniff is
// non-nil, each chunk is also fed to it to meter usage off the stream; sniffing
// never blocks or alters the bytes sent to the client.
func (s *Server) streamResponse(w http.ResponseWriter, resp *http.Response, sniff *usageSniffer) {
	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	rc := http.NewResponseController(w)
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			sniff.Write(buf[:n]) // nil-safe; a copy is not retained past the call
			_ = rc.Flush()       // ignore ErrNotSupported for non-flushable writers
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.log.Debug("stream copy ended", "err", err)
			}
			return
		}
	}
}

// streamAndRecord delivers a non-streaming upstream response to the client and
// hands the (original request, response) pair to the sidecar so it can populate
// the response cache and report usage. Recording is fire-and-forget on a
// detached context — it must never delay or fail the client's response.
func (s *Server) streamAndRecord(w http.ResponseWriter, resp *http.Response,
	rec *optimizer.RecordRequest, apiKey string, report cloud.UsageReport, clientCached bool,
	trackCosts bool) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, s.cfg.Proxy.MaxBodyBytes))
	if err != nil {
		// Fall back to a plain stream of whatever we did read; don't record a partial.
		s.streamResponse(w, resp, nil)
		return
	}
	for k, vals := range resp.Header {
		// We buffer the whole body and set our own length, so drop the upstream's
		// framing headers to avoid a Content-Length/Transfer-Encoding conflict.
		if http.CanonicalHeaderKey(k) == "Content-Length" || http.CanonicalHeaderKey(k) == "Transfer-Encoding" {
			continue
		}
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-Brevitas-Cache", "miss")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)

	// Meter the real usage the provider reported (cache-read/write tokens and
	// the dollars they saved) so `bvx stats` can answer "did caching help".
	usage := extractUsage(Family(report.Provider), body)
	s.stats.recordUsage(Family(report.Provider), report.Model, usage, clientCached, trackCosts)
	if trackCosts {
		s.reportCloud(apiKey, reportWithUsage(report, usage))
	}

	if rec == nil || s.opt == nil {
		return
	}
	rec.Response = body
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.opt.Record(ctx, rec); err != nil {
			s.log.Debug("cache record failed", "err", err)
		}
	}()
}

func (s *Server) reportCloud(apiKey string, report cloud.UsageReport) {
	if apiKey == "" || s.report == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.report(ctx, apiKey, report); err != nil {
			s.log.Debug("cloud usage report failed", "err", err)
		}
	}()
}

func (s *Server) writeError(w http.ResponseWriter, code int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	s.log.Warn("proxy error", "code", code, "msg", msg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	fmt.Fprintf(w, `{"error":{"type":"brevitas_proxy_error","message":%q}}`, msg)
}

// Health reports whether the proxy's own listener is accepting connections.
func (s *Server) Health(ctx context.Context) error {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", s.httpSrv.Addr)
	if err != nil {
		return err
	}
	return conn.Close()
}
