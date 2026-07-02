package proxy

import (
	"net/http"
	"strings"
)

// Family identifies an upstream API dialect the proxy understands.
type Family string

const (
	FamilyOpenAI    Family = "openai"
	FamilyAnthropic Family = "anthropic"
	FamilyGoogle    Family = "google"
	FamilyUnknown   Family = ""
)

// route describes where and how to forward a request.
type route struct {
	Family Family
	// Path is the upstream path (and query) to forward to.
	Path string
}

// classify inspects the request path and headers to determine the upstream
// family. Detection is header- and path-based so it works for OpenAI-,
// Anthropic-, and Google-compatible clients without any per-tool coupling.
func classify(r *http.Request) route {
	path := r.URL.Path
	rt := route{Path: path}
	if r.URL.RawQuery != "" {
		rt.Path = path + "?" + r.URL.RawQuery
	}

	switch {
	// Anthropic Messages API.
	case strings.HasPrefix(path, "/v1/messages"),
		strings.HasPrefix(path, "/v1/complete"),
		r.Header.Get("x-api-key") != "" && r.Header.Get("anthropic-version") != "":
		rt.Family = FamilyAnthropic

	// Google Generative Language API (Gemini).
	case strings.HasPrefix(path, "/v1beta/"),
		strings.HasPrefix(path, "/v1/models/") && strings.Contains(path, ":generate"),
		strings.Contains(path, ":streamGenerateContent"):
		rt.Family = FamilyGoogle

	// OpenAI-compatible (chat/completions, responses, embeddings, models...).
	case strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/openai/"):
		rt.Family = FamilyOpenAI

	default:
		rt.Family = FamilyUnknown
	}

	return rt
}

// String satisfies fmt.Stringer.
func (f Family) String() string { return string(f) }
