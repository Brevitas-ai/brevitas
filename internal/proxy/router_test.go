package proxy

import (
	"net/http"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		path    string
		headers map[string]string
		want    Family
	}{
		{"/v1/chat/completions", nil, FamilyOpenAI},
		{"/v1/embeddings", nil, FamilyOpenAI},
		{"/v1/messages", nil, FamilyAnthropic},
		{"/v1beta/models/gemini-pro:generateContent", nil, FamilyGoogle},
		{"/v1/messages", map[string]string{"anthropic-version": "2023-06-01", "x-api-key": "k"}, FamilyAnthropic},
		{"/openai/chat/completions", nil, FamilyOpenAI},
		{"/healthz", nil, FamilyUnknown},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(http.MethodPost, "http://x"+c.path, nil)
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		if got := classify(req).Family; got != c.want {
			t.Errorf("classify(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestClassifyRewritesAgentmapNamespace(t *testing.T) {
	// agentmap routes OpenAI as <proxy>/openai/... — must forward as /v1/...
	req, _ := http.NewRequest(http.MethodPost, "http://x/openai/chat/completions?a=1", nil)
	rt := classify(req)
	if rt.Family != FamilyOpenAI {
		t.Fatalf("family = %q", rt.Family)
	}
	if rt.Path != "/v1/chat/completions?a=1" {
		t.Errorf("rewritten path = %q, want /v1/chat/completions?a=1", rt.Path)
	}
}

func TestUpstreamURL(t *testing.T) {
	u, err := upstreamURL("https://api.openai.com", "/v1/chat/completions?foo=bar")
	if err != nil {
		t.Fatal(err)
	}
	if u.String() != "https://api.openai.com/v1/chat/completions?foo=bar" {
		t.Fatalf("got %s", u.String())
	}
}

func TestApplyGatewayAuth(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://x", nil)
	req.Header.Set("Authorization", "Bearer provider-openai")
	applyGatewayAuth(req, FamilyOpenAI, "sk-1")
	if req.Header.Get("X-Brevitas-Key") != "sk-1" || req.Header.Get("Authorization") != "Bearer provider-openai" {
		t.Errorf("openai auth = %q", req.Header.Get("Authorization"))
	}

	req2, _ := http.NewRequest(http.MethodPost, "http://x", nil)
	req2.Header.Set("x-api-key", "provider-anthropic")
	applyGatewayAuth(req2, FamilyAnthropic, "sk-2")
	if req2.Header.Get("X-Brevitas-Key") != "sk-2" || req2.Header.Get("x-api-key") != "provider-anthropic" {
		t.Errorf("anthropic auth = %q", req2.Header.Get("x-api-key"))
	}
}

func TestCopyRequestHeadersForwardsCredentials(t *testing.T) {
	src := http.Header{}
	src.Set("Authorization", "Bearer sk-user")
	src.Set("x-api-key", "sk-ant-user")
	src.Set("Connection", "keep-alive") // hop-by-hop, must be dropped
	dst := http.Header{}
	copyRequestHeaders(dst, src)

	if dst.Get("Authorization") != "Bearer sk-user" {
		t.Errorf("Authorization not forwarded: %q", dst.Get("Authorization"))
	}
	if dst.Get("x-api-key") != "sk-ant-user" {
		t.Errorf("x-api-key not forwarded: %q", dst.Get("x-api-key"))
	}
	if dst.Get("Connection") != "" {
		t.Error("hop-by-hop Connection header should be dropped")
	}
}
