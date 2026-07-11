package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Brevitas-ai/brevitas/internal/cloud"
	"github.com/Brevitas-ai/brevitas/internal/config"
	"github.com/Brevitas-ai/brevitas/internal/optimizer"
)

// fakeOptimizer rewrites the model to prove the optimize hook is applied.
type fakeOptimizer struct {
	called   bool
	fail     bool
	cacheHit []byte // when non-nil, Optimize returns a cache hit with this body
	recorded chan *optimizer.RecordRequest
}

func (f *fakeOptimizer) Optimize(_ context.Context, req *optimizer.Request) (*optimizer.Response, error) {
	f.called = true
	if f.fail {
		return nil, io.ErrUnexpectedEOF
	}
	if f.cacheHit != nil {
		return &optimizer.Response{CacheHit: true, CachedResponse: f.cacheHit, CacheKind: "exact"}, nil
	}
	body := map[string]any{}
	_ = json.Unmarshal(req.Body, &body)
	body["model"] = "optimized-model"
	out, _ := json.Marshal(body)
	return &optimizer.Response{Body: out, Applied: []string{"remodel"}}, nil
}
func (f *fakeOptimizer) Health(context.Context) error            { return nil }
func (f *fakeOptimizer) Version(context.Context) (string, error) { return "test", nil }
func (f *fakeOptimizer) Record(_ context.Context, req *optimizer.RecordRequest) error {
	if f.recorded != nil {
		f.recorded <- req
	}
	return nil
}

func newTestServer(t *testing.T, upstream string, opt optimizer.Client) *httptest.Server {
	t.Helper()
	cfg := config.Default()
	cfg.Upstreams["openai"] = upstream
	srv := New(Options{
		Config:      cfg,
		Optimizer:   opt,
		APIKey:      func(context.Context) (string, error) { return "sk-brevitas", nil },
		ReportUsage: func(context.Context, string, cloud.UsageReport) error { return nil },
	})
	return httptest.NewServer(srv.Handler())
}

func TestProxyReportsTenantScopedCloudReceipt(t *testing.T) {
	type captured struct {
		key    string
		report cloud.UsageReport
	}
	reports := make(chan captured, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","usage":{"prompt_tokens":20,"prompt_tokens_details":{"cached_tokens":5},"completion_tokens":2}}`)
	}))
	defer upstream.Close()
	cfg := config.Default()
	cfg.Upstreams["openai"] = upstream.URL
	srv := New(Options{
		Config: cfg, Optimizer: &fakeOptimizer{},
		APIKey: func(context.Context) (string, error) { return "bvt_customer", nil },
		ReportUsage: func(_ context.Context, key string, report cloud.UsageReport) error {
			reports <- captured{key, report}
			return nil
		},
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"private prompt"}]}`))
	req.Header.Set("X-Brevitas-Project", "billing-app")
	req.Header.Set("X-Brevitas-Client", "codex")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	select {
	case got := <-reports:
		if got.key != "bvt_customer" || got.report.Project != "billing-app" || got.report.Repo != "billing-app" || got.report.Client != "codex" {
			t.Fatalf("wrong tenant labels: %#v", got)
		}
		if got.report.FreshInputTokens != 15 || got.report.CachedInputTokens != 5 || got.report.OutputTokens != 2 {
			t.Fatalf("wrong receipt: %#v", got.report)
		}
		encoded, _ := json.Marshal(got.report)
		if bytes.Contains(encoded, []byte("private prompt")) {
			t.Fatal("cloud receipt contained model content")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cloud receipt was not reported")
	}
}

func TestCloudReceiptUsesSafeRepoOverride(t *testing.T) {
	t.Setenv("BREVITAS_REPO", "/private/customer/checkout-service")
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	report := newCloudReport(req, FamilyOpenAI, "gpt-4o-mini", nil, nil, false)
	if report.Repo != "checkout-service" || report.Project != "checkout-service" {
		t.Fatalf("unsafe repo labels: %#v", report)
	}
}

func TestProxyReportsStreamingCloudReceipt(t *testing.T) {
	reports := make(chan cloud.UsageReport, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"usage\":{\"prompt_tokens\":12,\"prompt_tokens_details\":{\"cached_tokens\":4},\"completion_tokens\":3}}\n\n")
	}))
	defer upstream.Close()
	cfg := config.Default()
	cfg.Upstreams["openai"] = upstream.URL
	srv := New(Options{
		Config: cfg, Optimizer: &fakeOptimizer{},
		APIKey: func(context.Context) (string, error) { return "bvt_stream", nil },
		ReportUsage: func(_ context.Context, _ string, report cloud.UsageReport) error {
			reports <- report
			return nil
		},
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"gpt-4o-mini","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	select {
	case report := <-reports:
		if !report.IsStream || report.CachedInputTokens != 4 || report.OutputTokens != 3 {
			t.Fatalf("wrong streaming receipt: %#v", report)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("streaming cloud receipt was not reported")
	}
}

func TestProxyOptimizesAndForwards(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	opt := &fakeOptimizer{}
	ts := newTestServer(t, upstream.URL, opt)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-user-real") // the tool's own key
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !opt.called {
		t.Error("optimizer was not called")
	}
	if gotBody["model"] != "optimized-model" {
		t.Errorf("upstream model = %v, want optimized-model", gotBody["model"])
	}
	// Passthrough (default): the tool's own credential reaches the provider
	// unchanged — Brevitas must not substitute its own key.
	if gotAuth != "Bearer sk-user-real" {
		t.Errorf("upstream auth = %q, want passthrough of the tool's key", gotAuth)
	}
}

func TestProxyFailsOpenWhenOptimizerErrors(t *testing.T) {
	var gotModel any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &b)
		gotModel = b["model"]
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	ts := newTestServer(t, upstream.URL, &fakeOptimizer{fail: true})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"original-model"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Fail-open: the original (unoptimized) body must still reach the upstream.
	if gotModel != "original-model" {
		t.Errorf("upstream model = %v, want original-model (fail-open)", gotModel)
	}
}

func TestProxyStreamsSSE(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for i := 0; i < 3; i++ {
			_, _ = io.WriteString(w, "data: chunk\n\n")
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	defer upstream.Close()

	ts := newTestServer(t, upstream.URL, &fakeOptimizer{})
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"gpt-4o","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	sc := bufio.NewScanner(resp.Body)
	count := 0
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "data:") {
			count++
		}
	}
	if count != 3 {
		t.Errorf("received %d SSE chunks, want 3", count)
	}
}

func TestProxyEmptyBodySkipsOptimizer(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	opt := &fakeOptimizer{}
	ts := newTestServer(t, upstream.URL, opt)
	defer ts.Close()

	// Empty body must not attempt optimization (json.RawMessage("") would error).
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if opt.called {
		t.Error("optimizer should be skipped for an empty body")
	}
}

func TestProxyCacheHitSkipsUpstream(t *testing.T) {
	var upstreamCalled bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalled = true
		_, _ = w.Write([]byte(`{"upstream":true}`))
	}))
	defer upstream.Close()

	cached := []byte(`{"cached":true}`)
	opt := &fakeOptimizer{cacheHit: cached}
	ts := newTestServer(t, upstream.URL, opt)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if upstreamCalled {
		t.Error("upstream MUST NOT be called on a cache hit")
	}
	if string(body) != string(cached) {
		t.Errorf("body = %s, want cached response", body)
	}
	if resp.Header.Get("X-Brevitas-Cache") != "hit" {
		t.Errorf("X-Brevitas-Cache = %q, want hit", resp.Header.Get("X-Brevitas-Cache"))
	}
}

func TestProxyRecordsNonStreamResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answer":42}`))
	}))
	defer upstream.Close()

	recorded := make(chan *optimizer.RecordRequest, 1)
	opt := &fakeOptimizer{recorded: recorded}
	ts := newTestServer(t, upstream.URL, opt)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	select {
	case record := <-recorded:
		if string(record.Response) != `{"answer":42}` {
			t.Errorf("recorded response = %q", record.Response)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Record was not called for a non-stream 200")
	}
}

func TestProxyMetersUsageIntoStats(t *testing.T) {
	// A non-stream completion whose usage block reports cached prompt tokens must
	// surface on the /__brevitas/stats endpoint as real cache-read tokens + $.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usage":{"prompt_tokens":1000,"completion_tokens":20,` +
			`"prompt_tokens_details":{"cached_tokens":800}}}`))
	}))
	defer upstream.Close()

	ts := newTestServer(t, upstream.URL, &fakeOptimizer{})
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	statsResp, err := http.Get(ts.URL + "/__brevitas/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer statsResp.Body.Close()
	var snap Snapshot
	if err := json.NewDecoder(statsResp.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.CacheReadTokens != 800 {
		t.Errorf("cache_read_tokens = %d, want 800", snap.CacheReadTokens)
	}
	if snap.InputTokens != 200 {
		t.Errorf("input_tokens = %d, want 200", snap.InputTokens)
	}
	if snap.PricedResponses != 1 || snap.CostSavedUSD <= 0 {
		t.Errorf("priced=%d cost=$%f, want 1 priced response with positive savings",
			snap.PricedResponses, snap.CostSavedUSD)
	}
}

func TestProxyMetersUsageThroughGzip(t *testing.T) {
	// Regression: the Anthropic/OpenAI SDKs send Accept-Encoding: gzip. If the
	// proxy forwarded that header, Go's transport would hand back a gzipped body
	// and usage metering would parse nothing. The proxy must strip it so the
	// transport decodes the body — proving metering still works when the upstream
	// gzips its response.
	// OpenAI usage shape: cached_tokens is a subset of prompt_tokens.
	usage := []byte(`{"usage":{"prompt_tokens":1000,"completion_tokens":50,` +
		`"prompt_tokens_details":{"cached_tokens":800}}}`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, _ = gz.Write(usage)
		_ = gz.Close()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(buf.Bytes())
	}))
	defer upstream.Close()

	ts := newTestServer(t, upstream.URL, &fakeOptimizer{})
	defer ts.Close()

	// Client asks for gzip, exactly like the real SDKs do.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	var snap Snapshot
	sr, err := http.Get(ts.URL + "/__brevitas/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Body.Close()
	_ = json.NewDecoder(sr.Body).Decode(&snap)
	if snap.CacheReadTokens != 800 || snap.InputTokens != 200 {
		t.Fatalf("usage not metered through gzip: read=%d input=%d (want 800, 200)",
			snap.CacheReadTokens, snap.InputTokens)
	}
}

func TestCopyRequestHeadersDropsAcceptEncoding(t *testing.T) {
	src := http.Header{}
	src.Set("Accept-Encoding", "gzip, br")
	src.Set("x-api-key", "keep-me")
	dst := http.Header{}
	copyRequestHeaders(dst, src)
	if dst.Get("Accept-Encoding") != "" {
		t.Errorf("Accept-Encoding should be dropped, got %q", dst.Get("Accept-Encoding"))
	}
	if dst.Get("x-api-key") != "keep-me" {
		t.Errorf("credentials must be preserved, got %q", dst.Get("x-api-key"))
	}
}

func TestProxyUnknownRouteIs404(t *testing.T) {
	ts := newTestServer(t, "http://127.0.0.1:1", &fakeOptimizer{})
	defer ts.Close()
	resp, err := http.Get(ts.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
