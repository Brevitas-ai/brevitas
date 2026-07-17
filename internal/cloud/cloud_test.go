package cloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeviceAuthorizationAndContentFreeReceipt(t *testing.T) {
	var gotKey string
	var gotReport UsageReport
	var gotRepo map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/device-auth/start":
			_ = json.NewEncoder(w).Encode(DeviceAuthorization{
				DeviceCode: "device_code", VerificationURIComplete: "https://example.test/#bvx=device_code",
				ExpiresIn: 60, Interval: 0,
			})
		case "/v1/device-auth/token":
			_ = json.NewEncoder(w).Encode(map[string]string{"api_key": "bvt_test"})
		case "/v1/usage":
			gotKey = r.Header.Get("X-Brevitas-Key")
			_ = json.NewDecoder(r.Body).Decode(&gotReport)
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		case "/v1/repositories":
			gotKey = r.Header.Get("X-Brevitas-Key")
			_ = json.NewDecoder(r.Body).Decode(&gotRepo)
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("BREVITAS_API_URL", server.URL)

	auth, err := StartDeviceAuthorization(context.Background())
	if err != nil || auth.DeviceCode != "device_code" {
		t.Fatalf("start = %#v, %v", auth, err)
	}
	key, pending, err := PollDeviceAuthorization(context.Background(), auth.DeviceCode)
	if err != nil || pending || key != "bvt_test" {
		t.Fatalf("poll = %q, %v, %v", key, pending, err)
	}
	report := UsageReport{Provider: "openai", Model: "gpt-4o-mini", BaselineTokens: 10,
		CompressedTokens: 8, FreshInputTokens: 8, RequestID: "request", Project: "app"}
	if err := ReportUsage(context.Background(), key, report); err != nil {
		t.Fatal(err)
	}
	if gotKey != key || gotReport.Project != "app" || gotReport.FreshInputTokens != 8 {
		t.Fatalf("reported key=%q receipt=%#v", gotKey, gotReport)
	}
	if err := RegisterRepository(context.Background(), key, "checkout-service"); err != nil {
		t.Fatal(err)
	}
	if gotKey != key || gotRepo["repo"] != "checkout-service" || gotRepo["source"] != "bvx" {
		t.Fatalf("registered key=%q repo=%#v", gotKey, gotRepo)
	}
}
