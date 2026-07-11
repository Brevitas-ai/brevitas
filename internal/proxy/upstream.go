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

// copyRequestHeaders copies safe headers from in to out, dropping internal
// Brevitas metadata, hop-by-hop headers, and connection-managed fields. It FORWARDS the
// caller's credentials (Authorization / x-api-key / x-goog-api-key): each AI
// tool already holds the user's real provider key, and Brevitas optimizes in
// the middle without touching authentication.
func copyRequestHeaders(dst http.Header, src http.Header) {
	for k, vals := range src {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-brevitas-") {
			continue
		}
		if _, hop := hopByHopHeaders[lk]; hop {
			continue
		}
		switch lk {
		case "host", "content-length":
			continue // set by the transport / recomputed for the new body
		case "accept-encoding":
			// Drop the caller's Accept-Encoding so Go's transport negotiates
			// compression itself and hands us a DECODED response body. If we
			// forwarded it (SDKs send "gzip"), the transport would leave the
			// body gzipped, and usage metering + response-cache storage would
			// choke on compressed bytes. The client hop is loopback, so losing
			// gzip there costs nothing.
			continue
		}
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

// applyGatewayAuth injects a single Brevitas key using each family's scheme.
// This is ONLY used in "inject" (gateway) mode, where the configured upstream
// is a Brevitas-managed gateway that holds the real provider keys. In the
// default "passthrough" mode it is not called and the tool's own credentials
// flow through unchanged.
func applyGatewayAuth(req *http.Request, family Family, apiKey string) {
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
