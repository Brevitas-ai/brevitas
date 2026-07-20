package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Brevitas-ai/brevitas/internal/cloud"
	"github.com/Brevitas-ai/brevitas/internal/config"
	"github.com/Brevitas-ai/brevitas/internal/keyring"
)

func TestLoadCustomerExportAcceptsHeterogeneousLegacyJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-customers.json")
	data := `[
  {"external_id":"legacy-001","display_name":"Alpha","email":"private@example.com"},
  {"customerId":"legacy-002","customerName":"Beta","password_hash":"must-not-upload"},
  {"user":{"user_id":"legacy-003"},"profile":{"company":"Gamma"}},
  {"account":{"account_id":1004},"name":"Delta"},
  {"client_id":"legacy-005","company_name":"Epsilon"},
  {"customer_id":"legacy-005","display_name":"Duplicate Epsilon"},
  {"email":"identity-is-not-an-email@example.com","name":"Rejected"}
]`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadCustomerExport(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RowsRead != 7 || len(loaded.Customers) != 5 || loaded.Duplicates != 1 || len(loaded.Invalid) != 1 {
		t.Fatalf("unexpected load summary: %#v", loaded)
	}
	got := map[string]string{}
	for _, customer := range loaded.Customers {
		got[customer.ExternalID] = customer.DisplayName
	}
	for _, id := range []string{"legacy-001", "legacy-002", "legacy-003", "1004", "legacy-005"} {
		if got[id] != "" {
			t.Errorf("customer %s unexpectedly uploaded display name %q", id, got[id])
		}
	}
	upload, _ := json.Marshal(loaded.Customers)
	for _, secret := range []string{"private@example.com", "must-not-upload"} {
		if bytes.Contains(upload, []byte(secret)) {
			t.Fatalf("unselected database field leaked: %s", upload)
		}
	}
}

func TestLoadCustomerExportSupportsCommonDatabaseExportFormats(t *testing.T) {
	tests := []struct {
		name string
		ext  string
		data string
		ids  []string
	}{
		{
			name: "semicolon csv", ext: ".csv",
			data: "customer_id;customer_name;email\ncsv-001;CSV One;one@example.com\ncsv-002;CSV Two;two@example.com\n",
			ids:  []string{"csv-001", "csv-002"},
		},
		{
			name: "json wrapper", ext: ".json",
			data: `{"records":[{"customer_id":"json-001","Name":"JSON One"},{"userId":"json-002","full_name":"JSON Two"}]}`,
			ids:  []string{"json-001", "json-002"},
		},
		{
			name: "json lines", ext: ".jsonl",
			data: "{\"clientId\":\"line-001\",\"clientName\":\"Line One\"}\n{\"memberId\":\"line-002\",\"displayName\":\"Line Two\"}\n",
			ids:  []string{"line-001", "line-002"},
		},
		{
			name: "keyed object", ext: ".json",
			data: `{"keyed-001":{"company":"Keyed One"},"keyed-002":"Keyed Two"}`,
			ids:  []string{"keyed-001", "keyed-002"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "customers"+test.ext)
			if err := os.WriteFile(path, []byte(test.data), 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, err := loadCustomerExport(path, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded.Invalid) != 0 || len(loaded.Customers) != len(test.ids) {
				t.Fatalf("loaded = %#v", loaded)
			}
			for index, id := range test.ids {
				if loaded.Customers[index].ExternalID != id {
					t.Fatalf("customer %d = %#v, want %q", index, loaded.Customers[index], id)
				}
			}
		})
	}
}

func TestLoadCustomerExportRequiresExplicitMappingForGenericOrAmbiguousIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ambiguous.json")
	if err := os.WriteFile(path, []byte(`[
  {"id":"membership-99","customer":{"id":"cust-123"}},
  {"customer_id":"cust-456","account_id":"account-456"}
]`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadCustomerExport(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Customers) != 0 || len(loaded.Invalid) != 2 {
		t.Fatalf("unsafe identities were accepted: %#v", loaded)
	}
	if !strings.Contains(loaded.Invalid[1], "--id-field") {
		t.Fatalf("ambiguous field guidance missing: %#v", loaded.Invalid)
	}

	explicit, err := loadCustomerExport(path, "customer.id", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(explicit.Customers) != 1 || explicit.Customers[0].ExternalID != "cust-123" {
		t.Fatalf("explicit mapping failed: %#v", explicit)
	}
}

func TestLoadCustomerExportBareObjectRequiresAndHonorsExplicitGenericID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "single-customer.json")
	if err := os.WriteFile(path, []byte(`{"id":"cust-1","name":"Alice"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	automatic, err := loadCustomerExport(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(automatic.Customers) != 0 || len(automatic.Invalid) != 1 {
		t.Fatalf("generic bare-object ID was not rejected: %#v", automatic)
	}
	explicit, err := loadCustomerExport(path, "id", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(explicit.Customers) != 1 || explicit.Customers[0].ExternalID != "cust-1" {
		t.Fatalf("explicit bare-object mapping failed: %#v", explicit)
	}
}

func TestLoadCustomerExportRejectsDirectDatabaseAndWorkbookFiles(t *testing.T) {
	for _, ext := range []string{".db", ".sqlite", ".xlsx"} {
		path := filepath.Join(t.TempDir(), "customers"+ext)
		if err := os.WriteFile(path, []byte("not uploaded"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadCustomerExport(path, "", ""); err == nil || !strings.Contains(err.Error(), "export only") {
			t.Fatalf("%s should require a safe export, got %v", ext, err)
		}
	}
}

func TestLoadCustomerExportSupportsExplicitNestedFieldMapping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-schema.json")
	if err := os.WriteFile(path, []byte(`[
  {"legacy":{"primary_key":"custom-001"},"business":{"label":"Custom One"}},
  {"legacy":{"primary_key":"custom-002"},"business":{"label":"Custom Two"}}
]`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadCustomerExport(path, "legacy.primary_key", "business.label")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Customers) != 2 || loaded.Customers[1].ExternalID != "custom-002" ||
		loaded.Customers[1].DisplayName != "Custom Two" {
		t.Fatalf("loaded = %#v", loaded)
	}
}

func TestOnboardCommandImportsOneHundredVariedPastCustomers(t *testing.T) {
	var gotKey string
	var imported []cloud.CustomerImport
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/customers/import" {
			http.NotFound(w, r)
			return
		}
		gotKey = r.Header.Get("X-Brevitas-Key")
		var input struct {
			Customers []cloud.CustomerImport `json:"customers"`
		}
		_ = json.NewDecoder(r.Body).Decode(&input)
		imported = append(imported, input.Customers...)
		_ = json.NewEncoder(w).Encode(map[string]int{"count": len(input.Customers)})
	}))
	defer server.Close()
	t.Setenv("BREVITAS_API_URL", server.URL)
	t.Setenv("BREVITAS_HOME", t.TempDir())

	records := make([]map[string]any, 0, 100)
	for index := 1; index <= 100; index++ {
		id := fmt.Sprintf("past-customer-%03d", index)
		switch index % 5 {
		case 0:
			records = append(records, map[string]any{"external_id": id, "display_name": fmt.Sprintf("Past %03d", index), "secret": "drop-me"})
		case 1:
			records = append(records, map[string]any{"customerId": id, "customerName": fmt.Sprintf("Past %03d", index)})
		case 2:
			records = append(records, map[string]any{"user": map[string]any{"user_id": id}, "profile": map[string]any{"company": fmt.Sprintf("Past %03d", index)}})
		case 3:
			records = append(records, map[string]any{"account_id": id, "account_name": fmt.Sprintf("Past %03d", index)})
		case 4:
			records = append(records, map[string]any{"clientId": id, "clientName": fmt.Sprintf("Past %03d", index)})
		}
	}
	data, _ := json.Marshal(map[string]any{"data": records})
	source := filepath.Join(t.TempDir(), "past-customers.json")
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}

	keys := keyring.NewMemory()
	var output bytes.Buffer
	app := &App{
		Cfg: config.Default(), Dirs: config.ResolveDirs(), Keyring: keys,
		In: strings.NewReader(""), Out: &output, Err: &output,
	}
	err := app.cmdOnboard(context.Background(), []string{
		"--customers", source, "--skip-scan", "--apply", "--api-key", "bvt_company_a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "bvt_company_a" || len(imported) != 100 {
		t.Fatalf("key=%q imported=%d", gotKey, len(imported))
	}
	seen := map[string]bool{}
	for _, customer := range imported {
		seen[customer.ExternalID] = true
	}
	for index := 1; index <= 100; index++ {
		if !seen[fmt.Sprintf("past-customer-%03d", index)] {
			t.Fatalf("customer %d was not imported", index)
		}
	}
	for _, fragment := range []string{
		"Valid customers", "100", "IMPORTING EXISTING CUSTOMERS", "Company onboarding complete",
	} {
		if !strings.Contains(output.String(), fragment) {
			t.Fatalf("output missing %q:\n%s", fragment, output.String())
		}
	}
}

func TestOnboardCommandBatchesMoreThanOneThousandCustomers(t *testing.T) {
	var batchSizes []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Customers []cloud.CustomerImport `json:"customers"`
		}
		_ = json.NewDecoder(r.Body).Decode(&input)
		batchSizes = append(batchSizes, len(input.Customers))
		_ = json.NewEncoder(w).Encode(map[string]int{"count": len(input.Customers)})
	}))
	defer server.Close()
	t.Setenv("BREVITAS_API_URL", server.URL)
	t.Setenv("BREVITAS_HOME", t.TempDir())

	records := make([]map[string]string, 1001)
	for index := range records {
		records[index] = map[string]string{"customer_id": fmt.Sprintf("bulk-%04d", index+1)}
	}
	data, _ := json.Marshal(records)
	source := filepath.Join(t.TempDir(), "customers.json")
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{
		Cfg: config.Default(), Dirs: config.ResolveDirs(), Keyring: keyring.NewMemory(),
		In: strings.NewReader(""), Out: io.Discard, Err: io.Discard,
	}
	if err := app.cmdOnboard(context.Background(), []string{
		"--customers", source, "--skip-scan", "--apply", "--api-key", "bvt_company_a",
	}); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(batchSizes) != "[1000 1]" {
		t.Fatalf("batch sizes = %v, want [1000 1]", batchSizes)
	}
}

func TestOnboardDryRunNeverImportsCustomers(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, "dry run must not call the API", http.StatusInternalServerError)
	}))
	defer server.Close()
	t.Setenv("BREVITAS_API_URL", server.URL)
	t.Setenv("BREVITAS_HOME", t.TempDir())
	source := filepath.Join(t.TempDir(), "customers.csv")
	if err := os.WriteFile(source, []byte("customer_id\npast-001\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	app := &App{
		Cfg: config.Default(), Dirs: config.ResolveDirs(), Keyring: keyring.NewMemory(),
		In: strings.NewReader(""), Out: &output, Err: &output,
	}
	if err := app.cmdOnboard(context.Background(), []string{
		"--customers", source, "--skip-scan",
	}); err != nil {
		t.Fatal(err)
	}
	if requests != 0 || !strings.Contains(output.String(), "DRY RUN COMPLETE") {
		t.Fatalf("requests=%d output=%s", requests, output.String())
	}
}

func TestOnboardCommandPromptsForCodebaseAndPastCustomerExport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test scanner uses a POSIX shell script")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/installations" {
			http.NotFound(w, r)
			return
		}
		var input map[string]any
		_ = json.NewDecoder(r.Body).Decode(&input)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"installation_id": input["installation_id"], "heartbeat_interval_seconds": 300,
		})
	}))
	defer server.Close()
	t.Setenv("BREVITAS_API_URL", server.URL)
	t.Setenv("BREVITAS_API_KEY", "bvt_company_a")
	t.Setenv("BREVITAS_HOME", t.TempDir())

	temp := t.TempDir()
	repo := filepath.Join(temp, "company-backend")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(temp, "past-customers.csv")
	if err := os.WriteFile(source, []byte("customer_id,customer_name\npast-001,Past One\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scanner := filepath.Join(temp, "agentmap")
	if err := os.WriteFile(scanner, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", temp+string(os.PathListSeparator)+os.Getenv("PATH"))

	var output bytes.Buffer
	app := &App{
		Cfg: config.Default(), Dirs: config.ResolveDirs(), Keyring: keyring.NewMemory(),
		In: strings.NewReader(repo + "\n" + source + "\n"), Out: &output, Err: &output,
	}
	if err := app.cmdOnboard(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{
		"GUIDED SETUP", "Backend project folder", "Drag a .csv", "YOUR INPUT", "setup ›", "DRY RUN COMPLETE",
	} {
		if !strings.Contains(output.String(), prompt) {
			t.Fatalf("interactive output missing %q:\n%s", prompt, output.String())
		}
	}
}

func TestOnboardingInputAcceptsShortAndFullGuideCommands(t *testing.T) {
	for _, input := range []string{
		"g", "guide", "--guide", "bvx onboard guide", "bvx   onboard   --guide",
	} {
		label, url, ok := onboardingResourceForInput(input)
		if !ok || label != "Onboarding guide" || url != onboardingGuideURL {
			t.Fatalf("guide input %q = %q, %q, %v", input, label, url, ok)
		}
	}
	for _, input := range []string{"d", "demo", "bvx onboard demo", "bvx onboard --demo"} {
		label, url, ok := onboardingResourceForInput(input)
		if !ok || label != "Dashboard demo" || url != dashboardDemoURL {
			t.Fatalf("demo input %q = %q, %q, %v", input, label, url, ok)
		}
	}
}

func TestDroppedCustomerExportPathIsNormalizedAndValidated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "past customers.csv")
	if err := os.WriteFile(path, []byte("customer_id\ncust-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, input := range []string{"'" + path + "'", `"` + path + `"`, strings.ReplaceAll(path, " ", `\ `)} {
		normalized := normalizeInteractivePath(input)
		if normalized != path {
			t.Fatalf("normalizeInteractivePath(%q) = %q, want %q", input, normalized, path)
		}
		if err := validateCustomerExportPath(normalized); err != nil {
			t.Fatalf("valid dropped path rejected: %v", err)
		}
	}

	bad := filepath.Join(t.TempDir(), "customers.xlsx")
	if err := os.WriteFile(bad, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateCustomerExportPath(bad); err == nil || !strings.Contains(err.Error(), "CSV") {
		t.Fatalf("unsupported export error = %v", err)
	}
}

func TestOnboardGuideAndDemoOpenInBrowser(t *testing.T) {
	previous := openBrowser
	defer func() { openBrowser = previous }()

	for _, test := range []struct {
		flag string
		want string
	}{
		{flag: "--guide", want: onboardingGuideURL},
		{flag: "--demo", want: dashboardDemoURL},
	} {
		t.Run(test.flag, func(t *testing.T) {
			opened := ""
			openBrowser = func(url string) error {
				opened = url
				return nil
			}
			var output bytes.Buffer
			app := &App{Cfg: config.Default(), In: strings.NewReader(""), Out: &output, Err: &output}
			if err := app.cmdOnboard(context.Background(), []string{test.flag}); err != nil {
				t.Fatal(err)
			}
			if opened != test.want || !strings.Contains(output.String(), "Opened") {
				t.Fatalf("opened=%q output=%q", opened, output.String())
			}
		})
	}
}

func TestOnboardCancellationReturnsToDashboard(t *testing.T) {
	var output bytes.Buffer
	app := &App{
		Cfg: config.Default(), In: strings.NewReader("q\n"), Out: &output, Err: &output,
		dashboardActive: true,
	}
	if err := app.cmdOnboard(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !app.returnHomeRequested || !strings.Contains(output.String(), "No files or customer records were changed") {
		t.Fatalf("cancel did not safely return Home: requested=%v output=%q", app.returnHomeRequested, output.String())
	}
}
