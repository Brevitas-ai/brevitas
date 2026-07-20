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

// applyGatewayAuth authenticates the Brevitas hop without overwriting the
// provider credential already copied from the caller.
func applyGatewayAuth(req *http.Request, _ Family, apiKey string) {
	if apiKey == "" {
		return
	}
	req.Header.Set("X-Brevitas-Key", apiKey)
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
