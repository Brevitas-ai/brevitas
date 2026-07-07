package proxy

import (
	"testing"
)

func TestExtractUsageAnthropic(t *testing.T) {
	body := []byte(`{"usage":{"input_tokens":100,"output_tokens":40,` +
		`"cache_read_input_tokens":900,"cache_creation_input_tokens":50}}`)
	u := extractUsage(FamilyAnthropic, body)
	if u.inputTokens != 100 || u.outputTokens != 40 || u.cacheRead != 900 || u.cacheWrite != 50 {
		t.Fatalf("anthropic usage = %+v", u)
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

func TestSavedMicroUSDAnthropic(t *testing.T) {
	// Opus input is $15/1M. saved = 15 * (0.9*1000 - 0.25*100) = 15 * 875 = 13125 micros.
	u := usage{cacheRead: 1000, cacheWrite: 100}
	got, known := savedMicroUSD(FamilyAnthropic, "claude-opus-4-8", u)
	if !known {
		t.Fatal("opus should be priceable")
	}
	if got != 13125 {
		t.Errorf("saved = %d micros, want 13125", got)
	}
}

func TestSavedMicroUSDCacheWritePenalty(t *testing.T) {
	// A pure cache-priming call (writes, no reads) costs money now — saved must
	// be negative, matching savings_from_usage's honest uncached-minus-actual.
	u := usage{cacheWrite: 1000}
	got, _ := savedMicroUSD(FamilyAnthropic, "claude-sonnet-4-6", u)
	if got >= 0 {
		t.Errorf("pure cache write should be negative savings, got %d", got)
	}
}

func TestSavedMicroUSDUnknownModel(t *testing.T) {
	if _, known := savedMicroUSD(FamilyAnthropic, "some-future-model", usage{cacheRead: 1000}); known {
		t.Error("unknown model must not be priced")
	}
}

func TestRecordUsageFoldsIntoSnapshot(t *testing.T) {
	s := newStats()
	s.recordUsage(FamilyAnthropic, "claude-opus-4-8", usage{inputTokens: 100, cacheRead: 900, cacheWrite: 0})
	snap := s.snapshot()
	if snap.CacheReadTokens != 900 || snap.InputTokens != 100 {
		t.Fatalf("tokens not recorded: %+v", snap)
	}
	// 900 read of 1000 total input = 90%.
	if snap.CacheReadPct < 89.9 || snap.CacheReadPct > 90.1 {
		t.Errorf("cache read pct = %.2f, want ~90", snap.CacheReadPct)
	}
	// 15 * 0.9 * 900 = 12150 micros = $0.01215.
	if snap.CostSavedUSD < 0.01214 || snap.CostSavedUSD > 0.01216 {
		t.Errorf("cost saved = %f, want ~0.01215", snap.CostSavedUSD)
	}
	if snap.PricedResponses != 1 {
		t.Errorf("priced responses = %d, want 1", snap.PricedResponses)
	}
}

func TestRecordUsageEmptyIsNoop(t *testing.T) {
	s := newStats()
	s.recordUsage(FamilyAnthropic, "claude-opus-4-8", usage{})
	if snap := s.snapshot(); snap.CacheReadTokens != 0 || snap.PricedResponses != 0 {
		t.Errorf("empty usage should not record anything: %+v", snap)
	}
}
