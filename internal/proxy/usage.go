package proxy

import (
	"bytes"
	"encoding/json"
	"strings"
)

// usage holds the real token counts a provider reports on a completion,
// normalized across dialects. inputTokens is the NON-cached prompt input;
// cacheRead is prompt input served from the provider's cache (billed cheaply);
// cacheWrite is input written INTO the cache on this call (billed at a premium).
type usage struct {
	inputTokens  int64
	outputTokens int64
	cacheRead    int64
	cacheWrite   int64
}

func (u usage) empty() bool { return u == usage{} }

// rawUsage is the union of the Anthropic and OpenAI usage shapes. Both providers
// nest it under "usage" (Anthropic also under "message.usage" in streaming
// message_start events). Zero values mean "not reported in this event".
type rawUsage struct {
	// Anthropic
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	// OpenAI-compatible
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	PromptTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	// OpenAI Responses API
	InputTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

// merge folds any non-zero field of o into r. Used to stitch a streaming
// response's usage together: Anthropic reports input/cache tokens in
// message_start and output tokens later in message_delta.
func (r *rawUsage) merge(o rawUsage) {
	if o.InputTokens != 0 {
		r.InputTokens = o.InputTokens
	}
	if o.OutputTokens != 0 {
		r.OutputTokens = o.OutputTokens
	}
	if o.CacheReadInputTokens != 0 {
		r.CacheReadInputTokens = o.CacheReadInputTokens
	}
	if o.CacheCreationInputTokens != 0 {
		r.CacheCreationInputTokens = o.CacheCreationInputTokens
	}
	if o.PromptTokens != 0 {
		r.PromptTokens = o.PromptTokens
	}
	if o.CompletionTokens != 0 {
		r.CompletionTokens = o.CompletionTokens
	}
	if o.PromptTokensDetails.CachedTokens != 0 {
		r.PromptTokensDetails.CachedTokens = o.PromptTokensDetails.CachedTokens
	}
	if o.InputTokensDetails.CachedTokens != 0 {
		r.InputTokensDetails.CachedTokens = o.InputTokensDetails.CachedTokens
	}
}

// normalize converts a raw usage block into the provider-neutral usage struct.
// For OpenAI, cached_tokens is a SUBSET of prompt_tokens, so it's split back out.
func (r rawUsage) normalize(family Family) usage {
	if family == FamilyAnthropic {
		return usage{
			inputTokens:  r.InputTokens,
			outputTokens: r.OutputTokens,
			cacheRead:    r.CacheReadInputTokens,
			cacheWrite:   r.CacheCreationInputTokens,
		}
	}
	prompt := r.PromptTokens
	output := r.CompletionTokens
	cached := r.PromptTokensDetails.CachedTokens
	if prompt == 0 && (r.InputTokens != 0 || r.OutputTokens != 0 || r.InputTokensDetails.CachedTokens != 0) {
		prompt = r.InputTokens
		output = r.OutputTokens
		cached = r.InputTokensDetails.CachedTokens
	}
	in := prompt - cached
	if in < 0 {
		in = 0
	}
	return usage{inputTokens: in, outputTokens: output, cacheRead: cached}
}

// extractUsage pulls the usage block out of a complete (non-streamed) provider
// response body. Best-effort: any parse problem yields a zero usage.
func extractUsage(family Family, body []byte) usage {
	var env struct {
		Usage rawUsage `json:"usage"`
	}
	if json.Unmarshal(body, &env) != nil {
		return usage{}
	}
	return env.Usage.normalize(family)
}

// usageSniffer extracts token usage from a streaming (SSE) response as bytes
// flow through, without buffering the whole body or disturbing the passthrough.
// It assembles data: lines across chunk boundaries and merges every usage block
// it sees. All errors are swallowed — metering must never break a stream.
type usageSniffer struct {
	family  Family
	partial []byte
	raw     rawUsage
}

func newUsageSniffer(family Family) *usageSniffer {
	return &usageSniffer{family: family}
}

// Write feeds a chunk of the streamed response. It never errors and never
// retains the caller's slice beyond the call.
func (sn *usageSniffer) Write(p []byte) {
	if sn == nil {
		return
	}
	sn.partial = append(sn.partial, p...)
	for {
		i := bytes.IndexByte(sn.partial, '\n')
		if i < 0 {
			break
		}
		line := sn.partial[:i]
		sn.partial = sn.partial[i+1:]
		sn.feedLine(line)
	}
	// Guard against an unterminated line growing without bound (e.g. a stream
	// with no newlines). A usage event is small; anything huge isn't one.
	if len(sn.partial) > 64<<10 {
		sn.feedLine(sn.partial)
		sn.partial = sn.partial[:0]
	}
}

func (sn *usageSniffer) feedLine(line []byte) {
	s := bytes.TrimSpace(line)
	if b, ok := bytes.CutPrefix(s, []byte("data:")); ok {
		s = bytes.TrimSpace(b)
	}
	if len(s) == 0 || s[0] != '{' {
		return
	}
	var ev struct {
		Usage   *rawUsage `json:"usage"`
		Message struct {
			Usage *rawUsage `json:"usage"`
		} `json:"message"`
		Response struct {
			Usage *rawUsage `json:"usage"`
		} `json:"response"`
	}
	if json.Unmarshal(s, &ev) != nil {
		return
	}
	if ev.Message.Usage != nil {
		sn.raw.merge(*ev.Message.Usage)
	}
	if ev.Usage != nil {
		sn.raw.merge(*ev.Usage)
	}
	if ev.Response.Usage != nil {
		sn.raw.merge(*ev.Response.Usage)
	}
}

// result returns the merged usage seen across the whole stream.
func (sn *usageSniffer) result() usage {
	if sn == nil {
		return usage{}
	}
	return sn.raw.normalize(sn.family)
}

// inputPricePerMillion returns the provider's list price in USD per 1M input
// tokens for a model, and whether the model is known. Prices as of 2026-07;
// deliberately a small, explicit table — an unknown model contributes real
// token counts to the stats but no dollar figure (better silent than wrong).
func inputPricePerMillion(model string) (float64, bool) {
	m := strings.ToLower(model)
	// Ordered most-specific first: "gpt-4o-mini" must beat "gpt-4o", and the
	// dated haiku ids must beat the bare "claude-haiku".
	for _, e := range []struct {
		sub   string
		price float64
	}{
		{"claude-opus", 15.0},
		{"claude-sonnet", 3.0},
		{"claude-3-5-haiku", 0.80},
		{"claude-3-haiku", 0.25},
		{"claude-haiku", 1.0},
		{"gpt-4o-mini", 0.15},
		{"gpt-4o", 2.50},
		{"gpt-4.1-mini", 0.40},
		{"gpt-4.1", 2.00},
		{"o3-mini", 1.10},
	} {
		if strings.Contains(m, e.sub) {
			return e.price, true
		}
	}
	return 0, false
}

// savedMicroUSD is the honest dollars saved by caching on one call, in
// micro-dollars (1e-6 USD), plus whether the model was priceable. It mirrors
// brevitas-systems' savings_from_usage: uncached_cost - actual_cost. Output
// tokens are billed identically either way and cancel out.
//
// Anthropic: a cache read costs 0.1x input and a cache write costs 1.25x input,
// so saved = price * (0.9*cacheRead - 0.25*cacheWrite) — negative on a pure
// cache-priming call, repaid as later reads land.
// OpenAI: a cached prompt token costs 0.5x, so saved = price * 0.5 * cacheRead.
func savedMicroUSD(family Family, model string, u usage) (int64, bool) {
	price, ok := inputPricePerMillion(model)
	if !ok {
		return 0, false
	}
	var factor float64
	if family == FamilyAnthropic {
		factor = 0.9*float64(u.cacheRead) - 0.25*float64(u.cacheWrite)
	} else {
		factor = 0.5 * float64(u.cacheRead)
	}
	// saved_usd = (price/1e6) * factor; micros = saved_usd * 1e6 = price * factor.
	return int64(price * factor), true
}
