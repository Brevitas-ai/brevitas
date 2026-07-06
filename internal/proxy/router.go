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

	// agentmap (codebase routing) namespaces providers by a leading segment,
	// e.g. OPENAI_BASE_URL=<proxy>/openai. Strip the namespace so the request
	// forwards to the provider's real path (e.g. /openai/chat/completions ->
	// /v1/chat/completions).
	var family Family
	switch {
	case strings.HasPrefix(path, "/openai/"):
		family = FamilyOpenAI
		path = "/v1/" + strings.TrimPrefix(path, "/openai/")
	case strings.HasPrefix(path, "/anthropic/"):
		family = FamilyAnthropic
		path = "/" + strings.TrimPrefix(path, "/anthropic/")
	case strings.HasPrefix(path, "/google/"):
		family = FamilyGoogle
		path = "/" + strings.TrimPrefix(path, "/google/")
	}

	rt := route{Path: path, Family: family}
	if r.URL.RawQuery != "" {
		rt.Path = path + "?" + r.URL.RawQuery
	}
	if family != FamilyUnknown {
		return rt
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
	case strings.HasPrefix(path, "/v1/"):
		rt.Family = FamilyOpenAI

	default:
		rt.Family = FamilyUnknown
	}

	return rt
}

// String satisfies fmt.Stringer.
func (f Family) String() string { return string(f) }
