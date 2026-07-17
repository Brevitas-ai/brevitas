// Package cloud contains the two small HTTPS calls bvx makes to Brevitas:
// browser authorization and content-free usage receipts.
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Brevitas-ai/brevitas/internal/version"
)

const defaultBaseURL = "https://brevitassystems.com"

var httpClient = &http.Client{Timeout: 10 * time.Second}

func baseURL() string {
	if value := os.Getenv("BREVITAS_API_URL"); value != "" {
		return strings.TrimRight(value, "/")
	}
	return defaultBaseURL
}

type DeviceAuthorization struct {
	DeviceCode              string `json:"device_code"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

func StartDeviceAuthorization(ctx context.Context) (*DeviceAuthorization, error) {
	var out DeviceAuthorization
	status, err := post(ctx, "/v1/device-auth/start", "", struct{}{}, &out)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || out.DeviceCode == "" || out.VerificationURIComplete == "" {
		return nil, fmt.Errorf("device authorization returned status %d", status)
	}
	return &out, nil
}

// PollDeviceAuthorization returns (key, pending, error).
func PollDeviceAuthorization(ctx context.Context, deviceCode string) (string, bool, error) {
	var out struct {
		APIKey string `json:"api_key"`
	}
	status, err := post(ctx, "/v1/device-auth/token", "",
		map[string]string{"device_code": deviceCode}, &out)
	if err != nil {
		return "", false, err
	}
	if status == http.StatusAccepted {
		return "", true, nil
	}
	if status != http.StatusOK || out.APIKey == "" {
		return "", false, fmt.Errorf("device authorization returned status %d", status)
	}
	return out.APIKey, false, nil
}

type UsageReport struct {
	Provider             string   `json:"provider"`
	Model                string   `json:"model"`
	Operation            string   `json:"operation"`
	BaselineTokens       int64    `json:"baseline_tokens"`
	CompressedTokens     int64    `json:"compressed_tokens"`
	BaselineOutputTokens *int64   `json:"baseline_output_tokens,omitempty"`
	FreshInputTokens     int64    `json:"fresh_input_tokens"`
	CachedInputTokens    int64    `json:"cached_input_tokens"`
	CacheWriteTokens     int64    `json:"cache_write_tokens"`
	CacheWrite5mTokens   int64    `json:"cache_write_5m_tokens"`
	CacheWrite1hTokens   int64    `json:"cache_write_1h_tokens"`
	CacheAttributable    bool     `json:"cache_attributable"`
	OutputTokens         int64    `json:"output_tokens"`
	QualityScore         *float64 `json:"quality_score,omitempty"`
	RequestID            string   `json:"request_id"`
	Strategy             string   `json:"strategy"`
	Project              string   `json:"project"`
	Environment          string   `json:"environment"`
	Source               string   `json:"source"`
	Repo                 string   `json:"repo"`
	Client               string   `json:"client"`
	Gateway              string   `json:"gateway"`
	ReceiptSource        string   `json:"receipt_source"`
	ReceiptAvailable     bool     `json:"receipt_available"`
	IsStream             bool     `json:"is_stream"`
}

func ReportUsage(ctx context.Context, apiKey string, report UsageReport) error {
	if apiKey == "" {
		return nil
	}
	status, err := post(ctx, "/v1/usage", apiKey, report, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("usage endpoint returned status %d", status)
	}
	return nil
}

func post(ctx context.Context, path, apiKey string, input, output any) (int, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL()+path, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", version.UserAgent())
	if apiKey != "" {
		req.Header.Set("X-Brevitas-Key", apiKey)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, err
	}
	if output != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 && len(body) > 0 {
		if err := json.Unmarshal(body, output); err != nil {
			return 0, err
		}
	}
	return resp.StatusCode, nil
}
