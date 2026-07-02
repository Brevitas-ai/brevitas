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

	"github.com/brevitas-systems/brevitas/internal/config"
	"github.com/brevitas-systems/brevitas/internal/optimizer"
)

// APIKeyFunc returns the Brevitas API key used for upstream auth.
type APIKeyFunc func(ctx context.Context) (string, error)

// Server is the local optimization proxy.
type Server struct {
	cfg       *config.Config
	opt       optimizer.Client
	apiKey    APIKeyFunc
	log       *slog.Logger
	transport *http.Transport
	httpSrv   *http.Server
}

// Options bundles the injected dependencies for a Server.
type Options struct {
	Config    *config.Config
	Optimizer optimizer.Client
	APIKey    APIKeyFunc
	Logger    *slog.Logger
}

// New builds a proxy Server. All dependencies are injected for testability.
func New(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.APIKey == nil {
		opts.APIKey = func(context.Context) (string, error) { return "", nil }
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
// SSE and chunked streaming reach the tool with minimal latency.
func (s *Server) streamResponse(w http.ResponseWriter, resp *http.Response) {
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
			_ = rc.Flush() // ignore ErrNotSupported for non-flushable writers
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.log.Debug("stream copy ended", "err", err)
			}
			return
		}
	}
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
