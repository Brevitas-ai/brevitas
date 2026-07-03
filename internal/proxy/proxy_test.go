package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Brevitas-ai/brevitas/internal/config"
	"github.com/Brevitas-ai/brevitas/internal/optimizer"
)

// fakeOptimizer rewrites the model to prove the optimize hook is applied.
type fakeOptimizer struct {
	called bool
	fail   bool
}

func (f *fakeOptimizer) Optimize(_ context.Context, req *optimizer.Request) (*optimizer.Response, error) {
	f.called = true
	if f.fail {
		return nil, io.ErrUnexpectedEOF
	}
	body := map[string]any{}
	_ = json.Unmarshal(req.Body, &body)
	body["model"] = "optimized-model"
	out, _ := json.Marshal(body)
	return &optimizer.Response{Body: out, Applied: []string{"remodel"}}, nil
}
func (f *fakeOptimizer) Health(context.Context) error            { return nil }
func (f *fakeOptimizer) Version(context.Context) (string, error) { return "test", nil }

func newTestServer(t *testing.T, upstream string, opt optimizer.Client) *httptest.Server {
	t.Helper()
	cfg := config.Default()
	cfg.Upstreams["openai"] = upstream
	srv := New(Options{
		Config:    cfg,
		Optimizer: opt,
		APIKey:    func(context.Context) (string, error) { return "sk-brevitas", nil },
	})
	return httptest.NewServer(srv.Handler())
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
