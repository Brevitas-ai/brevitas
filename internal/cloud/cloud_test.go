package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeviceAuthorizationAndContentFreeReceipt(t *testing.T) {
	var gotKey, gotCustomer string
	var gotReport UsageReport
	var gotRepo map[string]string
	var gotStart DeviceAuthorizationRequest
	var gotRegistration InstallationRegistration
	var gotHeartbeat InstallationHeartbeat
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/device-auth/start":
			_ = json.NewDecoder(r.Body).Decode(&gotStart)
			_ = json.NewEncoder(w).Encode(DeviceAuthorization{
				DeviceCode: "device_code", VerificationURIComplete: "https://example.test/#bvx=device_code",
				ExpiresIn: 60, Interval: 0,
			})
		case "/v1/device-auth/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"api_key": "bvt_test"})
		case "/v1/usage":
			gotKey = r.Header.Get("X-Brevitas-Key")
			gotCustomer = r.Header.Get("X-Brevitas-Customer-ID")
			_ = json.NewDecoder(r.Body).Decode(&gotReport)
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		case "/v1/repositories":
			gotKey = r.Header.Get("X-Brevitas-Key")
			_ = json.NewDecoder(r.Body).Decode(&gotRepo)
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		case "/v1/installations":
			gotKey = r.Header.Get("X-Brevitas-Key")
			_ = json.NewDecoder(r.Body).Decode(&gotRegistration)
			_ = json.NewEncoder(w).Encode(InstallationRegistrationResponse{
				InstallationID: gotRegistration.InstallationID, HeartbeatIntervalSecs: 300,
			})
		case "/v1/installations/ins_test/heartbeat":
			gotKey = r.Header.Get("X-Brevitas-Key")
			_ = json.NewDecoder(r.Body).Decode(&gotHeartbeat)
			_ = json.NewEncoder(w).Encode(InstallationHeartbeatResponse{Status: "active", HeartbeatIntervalSecs: 600})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("BREVITAS_API_URL", server.URL)

	device := Device("dev_test")
	auth, err := StartDeviceAuthorizationFor(context.Background(), device)
	if err != nil || auth.DeviceCode != "device_code" {
		t.Fatalf("start = %#v, %v", auth, err)
	}
	key, pending, err := PollDeviceAuthorization(context.Background(), auth.DeviceCode)
	if err != nil || pending || key != "bvt_test" {
		t.Fatalf("poll = %q, %v, %v", key, pending, err)
	}
	report := UsageReport{Provider: "openai", Model: "gpt-4o-mini", BaselineTokens: 10,
		CompressedTokens: 8, FreshInputTokens: 8, RequestID: "request", Project: "app",
		CustomerID: "cust_finance_01"}
	if err := ReportUsage(context.Background(), key, report); err != nil {
		t.Fatal(err)
	}
	if gotKey != key || gotCustomer != "cust_finance_01" || gotReport.Project != "app" || gotReport.FreshInputTokens != 8 {
		t.Fatalf("reported key=%q receipt=%#v", gotKey, gotReport)
	}
	if err := RegisterRepository(context.Background(), key, "checkout-service"); err != nil {
		t.Fatal(err)
	}
	if gotKey != key || gotRepo["repo"] != "checkout-service" || gotRepo["source"] != "bvx" {
		t.Fatalf("registered key=%q repo=%#v", gotKey, gotRepo)
	}
	if gotStart.Device != device || gotStart.Client.Name != "bvx" {
		t.Fatalf("device authorization metadata = %#v", gotStart)
	}
	registration := InstallationRegistration{
		InstallationID: "ins_test", Device: device,
		Repository:  RepositoryMetadata{ID: "repo_test", Label: "checkout-service"},
		Environment: "production", Client: Client(),
	}
	registered, err := RegisterInstallation(context.Background(), key, registration)
	if err != nil || registered.InstallationID != "ins_test" || registered.HeartbeatIntervalSecs != 300 {
		t.Fatalf("registration response = %#v, %v", registered, err)
	}
	if gotKey != key || gotRegistration.Repository.Label != "checkout-service" || gotRegistration.Environment != "production" {
		t.Fatalf("registration key=%q body=%#v", gotKey, gotRegistration)
	}
	heartbeat, err := HeartbeatInstallation(context.Background(), key, "ins_test", InstallationHeartbeat{
		Device: device, Environment: "production", Client: Client(),
	})
	if err != nil || heartbeat.Status != "active" || heartbeat.HeartbeatIntervalSecs != 600 {
		t.Fatalf("heartbeat response = %#v, %v", heartbeat, err)
	}
	if gotKey != key || gotHeartbeat.Device.ID != "dev_test" || gotHeartbeat.Environment != "production" {
		t.Fatalf("heartbeat key=%q body=%#v", gotKey, gotHeartbeat)
	}
}
