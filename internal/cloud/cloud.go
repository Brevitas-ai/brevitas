// Package cloud contains the two small HTTPS calls bvx makes to Brevitas:
// browser authorization and content-free usage receipts.
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Brevitas-ai/brevitas/internal/version"
)

const defaultBaseURL = "https://brevitassystems.com"

var httpClient = &http.Client{Timeout: 10 * time.Second}

var ErrInstallationUnsupported = errors.New("installation inventory endpoint unsupported")

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

// DeviceMetadata is deliberately pseudonymous. ID is a random value created
// by BVX; platform and architecture are operational compatibility fields.
// Hostnames, usernames, hardware identifiers, and network addresses are not
// collected.
type DeviceMetadata struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
}

type ClientMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type DeviceAuthorizationRequest struct {
	Device DeviceMetadata `json:"device"`
	Client ClientMetadata `json:"client"`
}

func Device(id string) DeviceMetadata {
	return DeviceMetadata{ID: id, Platform: runtime.GOOS, Arch: runtime.GOARCH}
}

func Client() ClientMetadata {
	return ClientMetadata{Name: "bvx", Version: version.Version}
}

func StartDeviceAuthorization(ctx context.Context) (*DeviceAuthorization, error) {
	return StartDeviceAuthorizationFor(ctx, DeviceMetadata{})
}

func StartDeviceAuthorizationFor(ctx context.Context, device DeviceMetadata) (*DeviceAuthorization, error) {
	var out DeviceAuthorization
	status, err := post(ctx, "/v1/device-auth/start", "", DeviceAuthorizationRequest{
		Device: device,
		Client: Client(),
	}, &out)
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
	return PollDeviceAuthorizationFor(ctx, deviceCode, DeviceMetadata{})
}

func PollDeviceAuthorizationFor(ctx context.Context, deviceCode string, device DeviceMetadata) (string, bool, error) {
	var out struct {
		APIKey string `json:"api_key"`
	}
	status, err := post(ctx, "/v1/device-auth/token", "",
		struct {
			DeviceCode string         `json:"device_code"`
			Device     DeviceMetadata `json:"device"`
			Client     ClientMetadata `json:"client"`
		}{DeviceCode: deviceCode, Device: device, Client: Client()}, &out)
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
	CustomerID           string   `json:"customer_id,omitempty"`
}

func ReportUsage(ctx context.Context, apiKey string, report UsageReport) error {
	if apiKey == "" {
		return nil
	}
	headers := http.Header{}
	if report.CustomerID != "" {
		headers.Set("X-Brevitas-Customer-ID", report.CustomerID)
	}
	status, err := postWithHeaders(ctx, "/v1/usage", apiKey, report, nil, headers)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("usage endpoint returned status %d", status)
	}
	return nil
}

// CustomerImport is the only customer data BVX uploads during onboarding.
// ExternalID must be Company A's stable opaque database identifier. No other
// database columns, credentials, or records are sent.
type CustomerImport struct {
	ExternalID  string `json:"external_id"`
	DisplayName string `json:"display_name,omitempty"`
}

type customerImportRequest struct {
	Customers []CustomerImport `json:"customers"`
}

type customerImportResponse struct {
	Count int `json:"count"`
}

func ImportCustomers(ctx context.Context, apiKey string, customers []CustomerImport) (int, error) {
	if apiKey == "" {
		return 0, errors.New("customer import requires a Brevitas organization key")
	}
	if len(customers) == 0 || len(customers) > 1000 {
		return 0, fmt.Errorf("customer import batch must contain 1 to 1000 customers")
	}
	var out customerImportResponse
	status, err := post(ctx, "/v1/customers/import", apiKey,
		customerImportRequest{Customers: customers}, &out)
	if err != nil {
		return 0, err
	}
	if status < 200 || status >= 300 {
		return 0, fmt.Errorf("customer import endpoint returned status %d", status)
	}
	if out.Count != len(customers) {
		return 0, fmt.Errorf("customer import acknowledged %d of %d rows", out.Count, len(customers))
	}
	return out.Count, nil
}

// RegisterRepository associates a safe repository label with the current
// account key. Absolute paths and source content never leave the machine.
func RegisterRepository(ctx context.Context, apiKey, repo string) error {
	repo = strings.TrimSpace(repo)
	if apiKey == "" || repo == "" {
		return nil
	}
	status, err := post(ctx, "/v1/repositories", apiKey, map[string]string{
		"repo":   repo,
		"source": "bvx",
	}, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("repository endpoint returned status %d", status)
	}
	return nil
}

// RepositoryMetadata is intentionally limited to a locally generated opaque
// ID and the final directory name. It never contains a path or Git remote.
type RepositoryMetadata struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type InstallationRegistration struct {
	InstallationID string             `json:"installation_id"`
	Device         DeviceMetadata     `json:"device"`
	Repository     RepositoryMetadata `json:"repository"`
	Environment    string             `json:"environment"`
	Client         ClientMetadata     `json:"client"`
}

type InstallationRegistrationResponse struct {
	InstallationID        string `json:"installation_id"`
	HeartbeatIntervalSecs int    `json:"heartbeat_interval_seconds"`
}

// RegisterInstallation binds safe AgentMap inventory metadata to the
// organization key. A 404/405 is reported to the caller so it can fall back to
// the legacy repository endpoint during a rolling backend upgrade.
func RegisterInstallation(ctx context.Context, apiKey string, input InstallationRegistration) (*InstallationRegistrationResponse, error) {
	if apiKey == "" {
		return nil, nil
	}
	var out InstallationRegistrationResponse
	status, err := post(ctx, "/v1/installations", apiKey, input, &out)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
			return nil, ErrInstallationUnsupported
		}
		return nil, fmt.Errorf("installation endpoint returned status %d", status)
	}
	if out.InstallationID == "" {
		out.InstallationID = input.InstallationID
	}
	return &out, nil
}

type InstallationHeartbeat struct {
	Device      DeviceMetadata `json:"device"`
	Environment string         `json:"environment"`
	Client      ClientMetadata `json:"client"`
}

type InstallationHeartbeatResponse struct {
	Status                string `json:"status"`
	HeartbeatIntervalSecs int    `json:"heartbeat_interval_seconds"`
}

func HeartbeatInstallation(ctx context.Context, apiKey, installationID string, input InstallationHeartbeat) (*InstallationHeartbeatResponse, error) {
	if apiKey == "" || installationID == "" {
		return nil, nil
	}
	var out InstallationHeartbeatResponse
	status, err := post(ctx, "/v1/installations/"+url.PathEscape(installationID)+"/heartbeat", apiKey, input, &out)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("installation heartbeat endpoint returned status %d", status)
	}
	return &out, nil
}

func post(ctx context.Context, path, apiKey string, input, output any) (int, error) {
	return postWithHeaders(ctx, path, apiKey, input, output, nil)
}

func postWithHeaders(ctx context.Context, path, apiKey string, input, output any, headers http.Header) (int, error) {
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
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
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
