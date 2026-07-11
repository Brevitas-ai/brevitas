package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Brevitas-ai/brevitas/internal/cloud"
	"github.com/Brevitas-ai/brevitas/internal/optimizer"
)

func newCloudReport(r *http.Request, family Family, model string,
	savings *optimizer.Savings, applied []string, stream bool) cloud.UsageReport {
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
		ReceiptSource: "proxy", IsStream: stream,
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

func reportWithUsage(report cloud.UsageReport, usage usage) cloud.UsageReport {
	input := usage.inputTokens + usage.cacheRead + usage.cacheWrite
	if report.BaselineTokens == 0 {
		report.BaselineTokens = input
		report.CompressedTokens = input
	}
	report.FreshInputTokens = usage.inputTokens
	report.CachedInputTokens = usage.cacheRead
	report.CacheWriteTokens = usage.cacheWrite
	report.OutputTokens = usage.outputTokens
	baselineOutput := usage.outputTokens
	report.BaselineOutputTokens = &baselineOutput
	report.ReceiptAvailable = !usage.empty()
	return report
}

func cacheHitCloudReport(r *http.Request, family Family, model string, cached []byte) cloud.UsageReport {
	usage := extractUsage(family, cached)
	report := newCloudReport(r, family, model, nil, []string{"exact_cache"}, false)
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
