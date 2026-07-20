package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Brevitas-ai/brevitas/internal/cloud"
	"github.com/Brevitas-ai/brevitas/internal/config"
	"github.com/Brevitas-ai/brevitas/internal/keyring"
)

func TestLoginOpensDashboardAndStoresReturnedKey(t *testing.T) {
	t.Setenv("BREVITAS_HOME", t.TempDir())
	var startRequest cloud.DeviceAuthorizationRequest
	var tokenDevice cloud.DeviceMetadata
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/device-auth/start":
			_ = json.NewDecoder(r.Body).Decode(&startRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code": "device_code", "verification_uri_complete": "https://example.test/dashboard#bvx=device_code",
				"expires_in": 10, "interval": 0,
			})
		case "/v1/device-auth/token":
			var body struct {
				Device cloud.DeviceMetadata `json:"device"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			tokenDevice = body.Device
			_ = json.NewEncoder(w).Encode(map[string]string{"api_key": "bvt_browser_login"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("BREVITAS_API_URL", server.URL)

	opened := ""
	previous := openBrowser
	openBrowser = func(url string) error { opened = url; return nil }
	defer func() { openBrowser = previous }()

	keys := keyring.NewMemory()
	var output bytes.Buffer
	app := &App{Cfg: config.Default(), Keyring: keys, Out: &output, Err: &output}
	if err := app.cmdLogin(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	stored, err := keys.Get(context.Background())
	if err != nil || stored != "bvt_browser_login" {
		t.Fatalf("stored key = %q, %v", stored, err)
	}
	if opened != "https://example.test/dashboard#bvx=device_code" {
		t.Fatalf("opened %q", opened)
	}
	if startRequest.Device.ID == "" || startRequest.Device.ID != tokenDevice.ID ||
		startRequest.Device.Platform == "" || startRequest.Device.Arch == "" ||
		startRequest.Client.Name != "bvx" {
		t.Fatalf("unsafe or unstable device metadata: start=%#v token=%#v", startRequest, tokenDevice)
	}
}
