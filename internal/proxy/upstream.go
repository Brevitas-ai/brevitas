package proxy

import (
	"net/http"
	"net/url"
	"strings"
)

// hopByHopHeaders must not be forwarded between client and upstream.
var hopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"proxy-connection":    {},
	"keep-alive":          {},
	"transfer-encoding":   {},
	"te":                  {},
	"trailer":             {},
	"upgrade":             {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
}

// copyRequestHeaders copies safe headers from in to out, dropping hop-by-hop
// headers and any inbound credentials (which are replaced with the upstream
// credential in applyUpstreamAuth).
func copyRequestHeaders(dst http.Header, src http.Header) {
	for k, vals := range src {
		lk := strings.ToLower(k)
		if _, hop := hopByHopHeaders[lk]; hop {
			continue
		}
		switch lk {
		case "authorization", "x-api-key", "x-goog-api-key", "host", "content-length":
			continue // set explicitly below / by the transport
		}
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

// applyUpstreamAuth injects the Brevitas API key into the outbound request
// using the scheme each provider family expects. The proxy authenticates to
// the (Brevitas-managed) upstream with a single key; it never fabricates or
// hacks around provider-native credentials.
func applyUpstreamAuth(req *http.Request, family Family, apiKey string) {
	if apiKey == "" {
		return
	}
	switch family {
	case FamilyAnthropic:
		req.Header.Set("x-api-key", apiKey)
		if req.Header.Get("anthropic-version") == "" {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
	case FamilyGoogle:
		// Gemini accepts the key via header (preferred over query string).
		req.Header.Set("x-goog-api-key", apiKey)
	default: // OpenAI-compatible
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

// upstreamURL joins an upstream base with a request path+query.
func upstreamURL(base, pathAndQuery string) (*url.URL, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	// Split the path/query we captured in the route.
	if i := strings.IndexByte(pathAndQuery, '?'); i >= 0 {
		u.Path = singleSlash(u.Path, pathAndQuery[:i])
		u.RawQuery = pathAndQuery[i+1:]
	} else {
		u.Path = singleSlash(u.Path, pathAndQuery)
	}
	return u, nil
}

func singleSlash(base, p string) string {
	base = strings.TrimRight(base, "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return base + p
}
