// Package optimizer is the Go-side client for the brevitas-systems Python
// package. It contains NO optimization logic; it only marshals requests to,
// and unmarshals responses from, the long-running brevitas-systems service.
//
// Architecture note: rather than launching a Python interpreter per request
// (which would add ~100ms+ of startup latency to every completion), the proxy
// talks to a persistent brevitas-systems process over a Unix domain socket
// (loopback TCP on Windows). This keeps per-request overhead in the
// single-digit-millisecond range, which matters for interactive coding tools.
package optimizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/brevitas-systems/brevitas/internal/config"
	"github.com/brevitas-systems/brevitas/internal/version"
)

// Request is a provider request handed to brevitas-systems for optimization.
type Request struct {
	// Provider is the upstream family: "openai", "anthropic", or "google".
	Provider string `json:"provider"`
	// Model is the requested model, extracted for routing/optimization.
	Model string `json:"model"`
	// Stream indicates the caller requested a streaming response.
	Stream bool `json:"stream"`
	// Path is the upstream API path, e.g. "/v1/chat/completions".
	Path string `json:"path"`
	// Headers are the inbound request headers (sanitized of the proxy auth).
	Headers map[string]string `json:"headers"`
	// Body is the raw JSON request body.
	Body json.RawMessage `json:"body"`
}

// Response is the optimized payload returned by brevitas-systems.
type Response struct {
	// Body is the (possibly) rewritten request body to forward upstream.
	Body json.RawMessage `json:"body"`
	// Headers are header overrides to apply before forwarding.
	Headers map[string]string `json:"headers"`
	// Applied lists the optimization passes brevitas-systems ran (for logs).
	Applied []string `json:"applied"`
	// Bypass, when true, tells the proxy to forward the original unchanged.
	Bypass bool `json:"bypass"`
}

// Client is a Client for the brevitas-systems optimization service.
type Client interface {
	// Optimize sends a request for optimization. On any failure the caller is
	// expected to fail open (forward the original), so errors are advisory.
	Optimize(ctx context.Context, req *Request) (*Response, error)
	// Health returns nil when the service is reachable and ready.
	Health(ctx context.Context) error
	// Version reports the running brevitas-systems version.
	Version(ctx context.Context) (string, error)
}

// httpClient talks HTTP to brevitas-systems over a Unix socket or TCP.
type httpClient struct {
	http *http.Client
	cfg  config.OptimizerConfig
	// base is the scheme+host used in request URLs; the host is ignored for
	// unix sockets but must be syntactically valid.
	base string
}

// New builds a Client from optimizer configuration.
func New(cfg config.OptimizerConfig) Client {
	transport := &http.Transport{
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 32,
		IdleConnTimeout:     90 * time.Second,
	}

	base := "http://brevitas-systems"
	switch cfg.Transport {
	case "unix":
		socket := cfg.Address
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}
	default: // tcp
		base = "http://" + cfg.Address
	}

	return &httpClient{
		http: &http.Client{Transport: transport, Timeout: cfg.CallTimeout},
		cfg:  cfg,
		base: base,
	}
}

func (c *httpClient) Optimize(ctx context.Context, req *Request) (*Response, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal optimize request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/optimize", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", version.UserAgent())

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call brevitas-systems: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 128<<20))
	if err != nil {
		return nil, fmt.Errorf("read optimize response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brevitas-systems returned %s: %s", resp.Status, truncate(body, 256))
	}

	var out Response
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode optimize response: %w", err)
	}
	return &out, nil
}

func (c *httpClient) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("brevitas-systems unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("brevitas-systems health status %s", resp.Status)
	}
	return nil
}

func (c *httpClient) Version(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/version", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var v struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	return v.Version, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
