package proxy

import (
	"encoding/json"
	"testing"
)

func TestExtractUsageAnthropic(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":100,"output_tokens":40,` +
		`"cache_read_input_tokens":900,"cache_creation_input_tokens":50,` +
		`"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":20}}}`)
	u := extractUsage(FamilyAnthropic, body)
	if u.inputTokens != 100 || u.outputTokens != 40 || u.cacheRead != 900 || u.cacheWrite != 50 ||
		u.cacheWrite5m != 30 || u.cacheWrite1h != 20 {
		t.Fatalf("anthropic usage = %+v", u)
	}
}

func TestExtractUsageDeepSeekCacheHitMissFields(t *testing.T) {
	body := []byte(`{"usage":{"prompt_cache_hit_tokens":800,"prompt_cache_miss_tokens":200,` +
		`"completion_tokens":30}}`)
	u := extractUsage(FamilyOpenAI, body)
	if u.inputTokens != 200 || u.cacheRead != 800 || u.outputTokens != 30 {
		t.Fatalf("deepseek usage = %+v", u)
	}
}

func TestExtractUsageOpenAISplitsCached(t *testing.T) {
	// OpenAI reports cached_tokens as a SUBSET of prompt_tokens.
	body := []byte(`{"usage":{"prompt_tokens":1000,"completion_tokens":30,` +
		`"prompt_tokens_details":{"cached_tokens":800}}}`)
	u := extractUsage(FamilyOpenAI, body)
	if u.cacheRead != 800 {
		t.Errorf("cacheRead = %d, want 800", u.cacheRead)
	}
	if u.inputTokens != 200 { // 1000 - 800 cached
		t.Errorf("inputTokens = %d, want 200", u.inputTokens)
	}
	if u.cacheWrite != 0 {
		t.Errorf("cacheWrite = %d, want 0 (openai has no write class)", u.cacheWrite)
	}
}

func TestExtractUsageOpenAIResponses(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":1000,"output_tokens":30,` +
		`"input_tokens_details":{"cached_tokens":800}}}`)
	u := extractUsage(FamilyOpenAI, body)
	if u.inputTokens != 200 || u.cacheRead != 800 || u.outputTokens != 30 {
		t.Fatalf("responses usage = %+v", u)
	}
}

func TestExtractUsageEmptyOnGarbage(t *testing.T) {
	if u := extractUsage(FamilyAnthropic, []byte("not json")); !u.empty() {
		t.Errorf("garbage body should yield empty usage, got %+v", u)
	}
}

func TestUsageSnifferStitchesAnthropicStream(t *testing.T) {
	// Anthropic streams input/cache tokens in message_start and output tokens
	// later in message_delta; the sniffer must merge both, across chunk splits.
	sn := newUsageSniffer(FamilyAnthropic)
	chunks := []string{
		"event: message_start\n",
		`data: {"type":"message_start","message":{"usage":{"input_tokens":10,`,
		`"cache_read_input_tokens":500,"cache_creation_input_tokens":20}}}` + "\n\n",
		"event: message_delta\n",
		`data: {"type":"message_delta","usage":{"output_tokens":33}}` + "\n\n",
	}
	for _, c := range chunks {
		sn.Write([]byte(c))
	}
	u := sn.result()
	if u.cacheRead != 500 || u.cacheWrite != 20 || u.inputTokens != 10 || u.outputTokens != 33 {
		t.Fatalf("sniffed usage = %+v", u)
	}
}

func TestUsageSnifferReadsOpenAIResponsesCompleted(t *testing.T) {
	sn := newUsageSniffer(FamilyOpenAI)
	sn.Write([]byte(`event: response.completed
data: {"type":"response.completed","response":{"usage":{"input_tokens":1200,"output_tokens":40,"input_tokens_details":{"cached_tokens":900}}}}

`))
	u := sn.result()
	if u.inputTokens != 300 || u.cacheRead != 900 || u.outputTokens != 40 {
		t.Fatalf("responses stream usage = %+v", u)
	}
}

func TestOptimizerReceiptPreservesStreamingCacheCategories(t *testing.T) {
	want := usage{inputTokens: 10, outputTokens: 7, cacheRead: 20, cacheWrite: 30,
		cacheWrite5m: 12, cacheWrite1h: 18}
	var raw rawUsage
	if err := json.Unmarshal(want.optimizerReceipt(FamilyAnthropic), &raw); err != nil {
		t.Fatal(err)
	}
	if got := raw.normalize(FamilyAnthropic); got != want {
		t.Fatalf("optimizer receipt = %+v, want %+v", got, want)
	}
}

func TestSavedMicroUSDAnthropicAttributable(t *testing.T) {
	// Opus 4.8 input is $5/1M. saved = 5 * (0.9*1000 - 0.25*100) = 5 * 875 = 4375 micros.
	u := usage{cacheRead: 1000, cacheWrite: 100}
	got, known := savedMicroUSD(FamilyAnthropic, "claude-opus-4-8", u, true)
	if !known {
		t.Fatal("opus should be priceable")
	}
	if got != 4375 {
		t.Errorf("saved = %d micros, want 4375", got)
	}
}

func TestSavedMicroUSDNotAttributableWhenClientCached(t *testing.T) {
	// The client set cache_control itself, so the reads happen with or without
	// Brevitas — Brevitas gets no credit even for a big cache read.
	u := usage{cacheRead: 100000}
	if got, known := savedMicroUSD(FamilyAnthropic, "claude-opus-4-8", u, false); known || got != 0 {
		t.Errorf("client-cached read must not be credited, got %d known=%v", got, known)
	}
}

func TestSavedMicroUSDOpenAINeverCredited(t *testing.T) {
	// OpenAI caches automatically; that is the provider's doing, never Brevitas's.
	if got, known := savedMicroUSD(FamilyOpenAI, "gpt-4o", usage{cacheRead: 100000}, true); known || got != 0 {
		t.Errorf("openai automatic caching must not be credited, got %d known=%v", got, known)
	}
}

func TestSavedMicroUSDCacheWritePenalty(t *testing.T) {
	// A pure cache-priming call (writes, no reads) costs money now — saved must
	// be negative, matching savings_from_usage's honest uncached-minus-actual.
	u := usage{cacheWrite: 1000}
	got, _ := savedMicroUSD(FamilyAnthropic, "claude-sonnet-4-6", u, true)
	if got >= 0 {
		t.Errorf("pure cache write should be negative savings, got %d", got)
	}
}

func TestSavedMicroUSDPricesOneHourWritePremium(t *testing.T) {
	// Sonnet input is $3/1M; a 1h write costs 2x, so the premium is
	// 3 * 1000 = 3000 micro-dollars rather than the 5m tier's 750.
	u := usage{cacheWrite: 1000, cacheWrite1h: 1000}
	got, _ := savedMicroUSD(FamilyAnthropic, "claude-sonnet-4-6", u, true)
	if got != -3000 {
		t.Errorf("1h write savings = %d micros, want -3000", got)
	}
}

func TestSavedMicroUSDUnknownModel(t *testing.T) {
	if _, known := savedMicroUSD(FamilyAnthropic, "some-future-model", usage{cacheRead: 1000}, true); known {
		t.Error("unknown model must not be priced")
	}
}

func TestRecordUsageCreditsBrevitasWhenClientDidNotCache(t *testing.T) {
	s := newStats()
	// clientCached=false: Brevitas inserted the breakpoints, so it earns credit.
	s.recordUsage(FamilyAnthropic, "claude-opus-4-8", usage{inputTokens: 100, cacheRead: 900}, false, true)
	snap := s.snapshot()
	if snap.CacheReadTokens != 900 || snap.InputTokens != 100 {
		t.Fatalf("tokens not recorded: %+v", snap)
	}
	if snap.AttributedCacheReadTokens != 900 || snap.ClientCachedReadTokens != 0 {
		t.Errorf("attribution wrong: attributed=%d client=%d", snap.AttributedCacheReadTokens, snap.ClientCachedReadTokens)
	}
	// 5 * 0.9 * 900 = 4050 micros = $0.00405.
	if snap.CostSavedUSD < 0.00404 || snap.CostSavedUSD > 0.00406 {
		t.Errorf("cost saved = %f, want ~0.00405", snap.CostSavedUSD)
	}
	if snap.PricedResponses != 1 {
		t.Errorf("priced responses = %d, want 1", snap.PricedResponses)
	}
}

func TestRecordUsageNoCreditWhenClientCached(t *testing.T) {
	s := newStats()
	// clientCached=true: the reads are the client's own caching. Tokens are still
	// measured, but Brevitas is credited $0 — this is the bug the fix addresses.
	s.recordUsage(FamilyAnthropic, "claude-opus-4-8", usage{inputTokens: 100, cacheRead: 900}, true, true)
	snap := s.snapshot()
	if snap.CacheReadTokens != 900 {
		t.Errorf("raw cache reads should still be measured, got %d", snap.CacheReadTokens)
	}
	if snap.ClientCachedReadTokens != 900 || snap.AttributedCacheReadTokens != 0 {
		t.Errorf("attribution wrong: attributed=%d client=%d", snap.AttributedCacheReadTokens, snap.ClientCachedReadTokens)
	}
	if snap.CostSavedUSD != 0 || snap.PricedResponses != 0 {
		t.Errorf("client-cached reads must credit Brevitas $0, got $%f over %d", snap.CostSavedUSD, snap.PricedResponses)
	}
}

func TestRecordUsageEmptyIsNoop(t *testing.T) {
	s := newStats()
	s.recordUsage(FamilyAnthropic, "claude-opus-4-8", usage{}, false, true)
	if snap := s.snapshot(); snap.CacheReadTokens != 0 || snap.PricedResponses != 0 {
		t.Errorf("empty usage should not record anything: %+v", snap)
	}
}

func TestRecordUsageDoesNotPriceSubscriptionTraffic(t *testing.T) {
	s := newStats()
	s.recordUsage(FamilyAnthropic, "claude-opus-4-8",
		usage{inputTokens: 100, cacheRead: 900}, false, false)

	snap := s.snapshot()
	if snap.InputTokens != 100 || snap.CacheReadTokens != 900 {
		t.Fatalf("subscription tokens were not measured: %+v", snap)
	}
	if snap.CostSavedUSD != 0 || snap.PricedResponses != 0 || snap.AttributedCacheReadTokens != 0 {
		t.Fatalf("subscription traffic was priced: %+v", snap)
	}
}
