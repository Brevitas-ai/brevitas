package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Brevitas-ai/brevitas/internal/cloud"
	"github.com/Brevitas-ai/brevitas/internal/optimizer"
)

func newCloudReport(r *http.Request, family Family, model string,
	savings *optimizer.Savings, applied []string, stream bool,
	cacheAttributable bool) cloud.UsageReport {
	repo := repoLabel(first(label(r, "X-Brevitas-Repo"), os.Getenv("BREVITAS_REPO")))
	project := repoLabel(first(label(r, "X-Brevitas-Project"), os.Getenv("BREVITAS_PROJECT")))
	if repo == "" {
		repo = project
	}
	if repo == "" {
		repo = repoLabel(gitRootName())
	}
	if project == "" {
		project = repo
	}
	if project == "" {
		project = "Unattributed"
	}
	if repo == "" {
		repo = project
	}
	client := first(label(r, "X-Brevitas-Client"), labelValue(os.Getenv("BREVITAS_CLIENT")))
	source := first(label(r, "X-Brevitas-Source"), labelValue(os.Getenv("BREVITAS_SOURCE")))
	if source == "" {
		source = client
	}
	if source == "" {
		source = "bvx"
	}
	if client == "" {
		client = source
	}
	strategy := strings.Join(applied, ",")
	if strategy == "" {
		strategy = "bvx_passthrough"
	}
	if len(strategy) > 64 {
		strategy = strategy[:64]
	}
	report := cloud.UsageReport{
		Provider: familyName(family, model), Model: model, Operation: operation(r.URL.Path, family),
		RequestID: requestID(r), Strategy: strategy, Project: project,
		Environment: defaultLabel(first(label(r, "X-Brevitas-Environment"), labelValue(os.Getenv("BREVITAS_ENVIRONMENT"))), "local"),
		Source:      source, Repo: repo, Client: client, Gateway: "bvx",
		ReceiptSource: "proxy", IsStream: stream, CacheAttributable: cacheAttributable,
		CustomerID: customerIDOrEmpty(r),
	}
	if savings != nil {
		report.BaselineTokens = int64(savings.TokensBefore)
		report.CompressedTokens = int64(savings.TokensAfter)
		if !savings.Lossy {
			quality := 1.0
			report.QualityScore = &quality
		}
	} else if contains(applied, "native_cache") {
		quality := 1.0
		report.QualityScore = &quality
	}
	return report
}

// customerAttribution accepts only an opaque, header-safe identifier. It does
// not infer identity from repository contents, paths, prompts, or provider
// credentials. The Brevitas backend still owns authorization: it binds this ID
// to the organization derived from the authenticated service key.
func customerAttribution(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.Header.Get("X-Brevitas-Customer-ID"))
	if value == "" {
		return "", nil
	}
	if len(value) > 200 {
		return "", fmt.Errorf("X-Brevitas-Customer-ID exceeds 200 bytes")
	}
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '_' || ch == '-' || ch == '.' || ch == ':' {
			continue
		}
		return "", fmt.Errorf("X-Brevitas-Customer-ID must be an opaque ASCII identifier")
	}
	return value, nil
}

func customerIDOrEmpty(r *http.Request) string {
	value, _ := customerAttribution(r)
	return value
}

func reportWithUsage(report cloud.UsageReport, usage usage) cloud.UsageReport {
	input := usage.inputTokens + usage.cacheRead + usage.cacheWrite
	// Provider usage is authoritative for the request actually billed, including
	// tools, system prompts, and provider-tokenizer overhead. Preserve only the
	// optimizer's before/after delta, whose local-tokenizer bias cancels, then
	// anchor both sides of the comparison to the same provider receipt.
	delta := report.BaselineTokens - report.CompressedTokens
	report.CompressedTokens = input
	report.BaselineTokens = input + delta
	if report.BaselineTokens < 0 {
		report.BaselineTokens = 0
	}
	report.FreshInputTokens = usage.inputTokens
	report.CachedInputTokens = usage.cacheRead
	report.CacheWriteTokens = usage.cacheWrite
	report.CacheWrite5mTokens = usage.cacheWrite5m
	report.CacheWrite1hTokens = usage.cacheWrite1h
	report.OutputTokens = usage.outputTokens
	baselineOutput := usage.outputTokens
	report.BaselineOutputTokens = &baselineOutput
	report.ReceiptAvailable = !usage.empty()
	return report
}

func cacheHitCloudReport(r *http.Request, family Family, model string, cached []byte) cloud.UsageReport {
	usage := extractUsage(family, cached)
	report := newCloudReport(r, family, model, nil, []string{"exact_cache"}, false, true)
	report.BaselineTokens = usage.inputTokens + usage.cacheRead + usage.cacheWrite
	report.CompressedTokens = 0
	baselineOutput := usage.outputTokens
	report.BaselineOutputTokens = &baselineOutput
	report.ReceiptAvailable = !usage.empty()
	quality := 1.0
	report.QualityScore = &quality
	return report
}

func requestID(r *http.Request) string {
	if value := r.Header.Get("X-Brevitas-Request-Id"); value != "" {
		return value
	}
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return ""
}

func operation(path string, family Family) string {
	switch {
	case strings.Contains(path, "/responses"):
		return "responses"
	case strings.Contains(path, "/embeddings"):
		return "embeddings"
	case family == FamilyAnthropic:
		return "messages"
	case family == FamilyGoogle:
		return "generate_content"
	default:
		return "chat.completions"
	}
}

func familyName(family Family, model string) string {
	if strings.HasPrefix(strings.ToLower(model), "deepseek") {
		return "deepseek"
	}
	return string(family)
}

func label(r *http.Request, name string) string {
	return labelValue(r.Header.Get(name))
}

func labelValue(raw string) string {
	value := strings.TrimSpace(raw)
	if filepath.IsAbs(value) {
		return filepath.Base(value)
	}
	if len(value) > 128 {
		return value[:128]
	}
	return value
}

func repoLabel(value string) string {
	value = strings.TrimSuffix(strings.ReplaceAll(strings.TrimSpace(value), "\\", "/"), "/")
	if slash := strings.LastIndexByte(value, '/'); slash >= 0 {
		value = value[slash+1:]
	}
	value = strings.TrimSuffix(value, ".git")
	return labelValue(value)
}

func gitRootName() string {
	directory, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, ".git")); err == nil {
			return filepath.Base(directory)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
		directory = parent
	}
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func defaultLabel(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
