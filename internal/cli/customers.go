package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Brevitas-ai/brevitas/internal/cloud"
)

const (
	maxCustomerExportBytes = 64 << 20
	maxCustomerRows        = 1_000_000
	customerImportBatch    = 1000
)

var customerIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,199}$`)

var customerIDAliases = []string{
	"externalid", "customerid", "userid", "clientid", "accountid",
	"memberid",
}

var customerArrayAliases = []string{
	"customers", "users", "clients", "accounts", "members",
	"records", "results", "items", "data",
}

type customerLoadResult struct {
	Customers  []cloud.CustomerImport
	Format     string
	RowsRead   int
	Duplicates int
	Invalid    []string
	IDFields   map[string]int
	NameFields map[string]int
}

func (a *App) cmdOnboard(ctx context.Context, args []string) error {
	if helpRequested(args) {
		a.printOnboardHelp()
		return nil
	}
	fs := flag.NewFlagSet("onboard", flag.ContinueOnError)
	fs.SetOutput(a.Err)
	source := fs.String("customers", "", "past-customer export (CSV, JSON, or JSONL)")
	idField := fs.String("id-field", "", "explicit stable customer ID field, including nested paths")
	nameField := fs.String("name-field", "", "explicit display-name field to opt into uploading, including nested paths")
	apply := fs.Bool("apply", false, "route the codebase and import customers after preview")
	auto := fs.Bool("auto", false, "with --apply, rewrite hardcoded provider URLs")
	skipInvalid := fs.Bool("skip-invalid", false, "import valid rows while reporting invalid rows")
	skipScan := fs.Bool("skip-scan", false, "import customer data without scanning a codebase")
	apiKeyFlag := fs.String("api-key", "", "Brevitas key for CI; otherwise use browser login")
	noOpen := fs.Bool("no-open", false, "do not open the AgentMap HTML report")
	target := fs.String("target", a.Cfg.ProxyURL(), "gateway URL to route calls through")
	environment := fs.String("environment", envOr("BREVITAS_ENVIRONMENT", "production"), "deployment environment")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("provide at most one codebase path")
	}
	reader := bufio.NewReader(a.In)
	ask := func(label string) (string, error) {
		fmt.Fprint(a.Out, label)
		line, err := reader.ReadString('\n')
		if err != nil && line == "" {
			return "", err
		}
		return strings.TrimSpace(line), nil
	}

	repo := ""
	if fs.NArg() == 1 {
		repo = fs.Arg(0)
	}
	if !*skipScan && repo == "" {
		value, err := ask("Company backend codebase path: ")
		if err != nil {
			return fmt.Errorf("read codebase path: %w", err)
		}
		repo = value
	}
	if !*skipScan && repo == "" {
		return errors.New("a codebase path is required unless --skip-scan is used")
	}
	if *source == "" {
		value, err := ask("Past-customer database export (CSV, JSON, or JSONL): ")
		if err != nil {
			return fmt.Errorf("read customer export path: %w", err)
		}
		*source = value
	}
	if *source == "" {
		return errors.New("a past-customer export is required")
	}

	loaded, err := loadCustomerExport(*source, *idField, *nameField)
	if err != nil {
		return err
	}
	a.page("Onboard a company backend", "Scan AI traffic and map existing customers by exact stable ID.")
	a.section("Customer export preview")
	a.metric("Format", loaded.Format, ansiCyan)
	a.metric("Rows read", fmt.Sprintf("%d", loaded.RowsRead), ansiCyan)
	a.metric("Valid customers", fmt.Sprintf("%d", len(loaded.Customers)), ansiGreen)
	a.metric("Exact duplicates", fmt.Sprintf("%d", loaded.Duplicates), ansiYellow)
	a.metric("Invalid rows", fmt.Sprintf("%d", len(loaded.Invalid)), ansiYellow)
	for _, issue := range firstStrings(loaded.Invalid, 10) {
		a.warn("%s", issue)
	}
	if len(loaded.IDFields) > 0 {
		a.note("Detected ID fields: %s", formatFieldCounts(loaded.IDFields))
	}
	if len(loaded.NameFields) > 0 {
		a.note("Detected name fields: %s", formatFieldCounts(loaded.NameFields))
	}
	a.note("Only external_id is uploaded by default. display_name is uploaded only with --name-field; no other database fields leave this machine.")

	if len(loaded.Invalid) > 0 && !*skipInvalid {
		return fmt.Errorf("%d invalid customer rows; fix the export or rerun with --skip-invalid", len(loaded.Invalid))
	}
	if len(loaded.Customers) == 0 {
		return errors.New("no valid customers found")
	}

	if !*skipScan {
		scanArgs := []string{"--target", *target, "--environment", *environment}
		if *apiKeyFlag != "" {
			scanArgs = append(scanArgs, "--api-key", *apiKeyFlag)
		}
		if *noOpen {
			scanArgs = append(scanArgs, "--no-open")
		}
		if *apply {
			scanArgs = append(scanArgs, "--apply")
		}
		if *auto {
			scanArgs = append(scanArgs, "--auto")
		}
		if err := a.installCodebase(ctx, repo, scanArgs); err != nil {
			return err
		}
	}

	if !*apply {
		a.section("Dry run complete")
		a.note("No customer records were imported and no routing files were changed.")
		a.command("bvx onboard --customers "+*source+" --apply "+repo,
			"Apply routing and import the validated customers")
		return nil
	}

	if *skipScan {
		if err := a.ensureAPIKey(ctx, *apiKeyFlag); err != nil {
			return err
		}
	}
	apiKey, err := a.apiKeyFunc()(ctx)
	if err != nil || apiKey == "" {
		return errors.New("Brevitas customer-import key is unavailable")
	}

	a.section("Importing existing customers")
	imported := 0
	for start := 0; start < len(loaded.Customers); start += customerImportBatch {
		end := min(start+customerImportBatch, len(loaded.Customers))
		count, importErr := cloud.ImportCustomers(ctx, apiKey, loaded.Customers[start:end])
		if importErr != nil {
			return fmt.Errorf("import customer batch %d: %w (reconnect with `bvx login` if this device predates customer-import authorization)", start/customerImportBatch+1, importErr)
		}
		imported += count
		a.ok("Imported %d of %d customers", imported, len(loaded.Customers))
	}

	a.success("Company onboarding complete")
	a.metric("Existing customers", fmt.Sprintf("%d", imported), ansiGreen)
	a.note("New customers will be provisioned automatically from X-Brevitas-Customer-ID on first AI traffic.")
	a.command("bvx status", "Verify the proxy and connected backend")
	return nil
}

func loadCustomerExport(path, idField, nameField string) (customerLoadResult, error) {
	result := customerLoadResult{IDFields: map[string]int{}, NameFields: map[string]int{}}
	info, err := os.Stat(path)
	if err != nil {
		return result, fmt.Errorf("open customer export: %w", err)
	}
	if !info.Mode().IsRegular() {
		return result, errors.New("customer export must be a regular file")
	}
	if info.Size() > maxCustomerExportBytes {
		return result, fmt.Errorf("customer export exceeds %d MiB", maxCustomerExportBytes>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return result, fmt.Errorf("read customer export: %w", err)
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if len(bytes.TrimSpace(data)) == 0 {
		return result, errors.New("customer export is empty")
	}

	ext := strings.ToLower(filepath.Ext(path))
	var records []map[string]any
	switch ext {
	case ".csv", ".tsv":
		result.Format = strings.TrimPrefix(ext, ".")
		records, err = parseCustomerCSV(data)
	case ".jsonl", ".ndjson":
		result.Format = "jsonl"
		records, err = parseCustomerJSONL(data)
	case ".json":
		result.Format = "json"
		records, err = parseCustomerJSON(data, idField)
	case ".db", ".sqlite", ".sqlite3", ".xls", ".xlsx":
		return result, fmt.Errorf("direct %s database/workbook reads are disabled; export only the stable customer ID and optional display name to CSV, JSON, or JSONL", ext)
	default:
		trimmed := bytes.TrimSpace(data)
		if trimmed[0] == '[' || trimmed[0] == '{' {
			result.Format = "json"
			records, err = parseCustomerJSON(data, idField)
		} else {
			result.Format = "delimited"
			records, err = parseCustomerCSV(data)
		}
	}
	if err != nil {
		return result, fmt.Errorf("parse customer export: %w", err)
	}
	if len(records) > maxCustomerRows {
		return result, fmt.Errorf("customer export exceeds %d rows", maxCustomerRows)
	}
	result.RowsRead = len(records)
	seen := make(map[string]int, len(records))
	for index, record := range records {
		id, idPath, identityIssue := customerIdentityField(record, idField)
		if identityIssue != "" {
			result.Invalid = append(result.Invalid, fmt.Sprintf("row %d: %s", index+1, identityIssue))
			continue
		}
		name, namePath := "", ""
		if nameField != "" {
			name, namePath = customerField(record, nameField, nil)
		}
		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		if id == "" {
			result.Invalid = append(result.Invalid, fmt.Sprintf("row %d: stable customer ID not found", index+1))
			continue
		}
		if !customerIDPattern.MatchString(id) {
			result.Invalid = append(result.Invalid, fmt.Sprintf("row %d: customer ID is not an opaque 1-200 character identifier", index+1))
			continue
		}
		if utf8.RuneCountInString(name) > 200 {
			result.Invalid = append(result.Invalid, fmt.Sprintf("row %d: display name exceeds 200 characters", index+1))
			continue
		}
		if existing, ok := seen[id]; ok {
			result.Duplicates++
			if result.Customers[existing].DisplayName == "" && name != "" {
				result.Customers[existing].DisplayName = name
			}
			continue
		}
		seen[id] = len(result.Customers)
		result.Customers = append(result.Customers, cloud.CustomerImport{
			ExternalID: id, DisplayName: name,
		})
		result.IDFields[idPath]++
		if namePath != "" {
			result.NameFields[namePath]++
		}
	}
	return result, nil
}

func parseCustomerCSV(data []byte) ([]map[string]any, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	reader.ReuseRecord = false
	reader.Comma = detectDelimiter(data)
	headers, err := reader.Read()
	if err != nil {
		return nil, err
	}
	for index := range headers {
		headers[index] = strings.TrimSpace(headers[index])
		if headers[index] == "" {
			return nil, fmt.Errorf("column %d has an empty header", index+1)
		}
	}
	var records []map[string]any
	for {
		row, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
		record := make(map[string]any, len(headers))
		for index, header := range headers {
			if index < len(row) {
				record[header] = strings.TrimSpace(row[index])
			}
		}
		records = append(records, record)
	}
	return records, nil
}

func detectDelimiter(data []byte) rune {
	line := string(data)
	if newline := strings.IndexByte(line, '\n'); newline >= 0 {
		line = line[:newline]
	}
	candidates := []rune{',', '\t', ';', '|'}
	best, count := ',', -1
	for _, candidate := range candidates {
		if current := strings.Count(line, string(candidate)); current > count {
			best, count = candidate, current
		}
	}
	return best
}

func parseCustomerJSON(data []byte, idField string) ([]map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return customerRecords(value, idField)
}

func parseCustomerJSONL(data []byte) ([]map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var records []map[string]any
	for {
		var value any
		err := decoder.Decode(&value)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		record, ok := value.(map[string]any)
		if !ok {
			record = map[string]any{}
		}
		records = append(records, record)
	}
	return records, nil
}

func customerRecords(value any, idField string) ([]map[string]any, error) {
	switch typed := value.(type) {
	case []any:
		records := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			record, ok := item.(map[string]any)
			if !ok {
				record = map[string]any{}
			}
			records = append(records, record)
		}
		return records, nil
	case map[string]any:
		if items, ok := findCustomerArray(typed, 0); ok {
			return customerRecords(items, idField)
		}
		if idField != "" {
			if value, _ := customerField(typed, idField, nil); value != "" {
				return []map[string]any{typed}, nil
			}
		}
		if value, _, issue := customerIdentityField(typed, ""); value != "" || issue != "" {
			return []map[string]any{typed}, nil
		}
		if hasGenericCustomerIdentity(typed) {
			return []map[string]any{typed}, nil
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		records := make([]map[string]any, 0, len(keys))
		for _, key := range keys {
			switch item := typed[key].(type) {
			case map[string]any:
				record := make(map[string]any, len(item)+1)
				for field, fieldValue := range item {
					record[field] = fieldValue
				}
				if value, _, issue := customerIdentityField(record, ""); value == "" && issue == "" {
					record["external_id"] = key
				}
				records = append(records, record)
			case string:
				records = append(records, map[string]any{"external_id": key, "display_name": item})
			default:
				records = append(records, map[string]any{"external_id": key})
			}
		}
		return records, nil
	default:
		return nil, errors.New("JSON export must contain customer objects")
	}
}

func hasGenericCustomerIdentity(record map[string]any) bool {
	for _, alias := range []string{"id", "uuid"} {
		if value, _, ok := findCustomerField(record, alias, "", 0); ok &&
			strings.TrimSpace(scalarString(value)) != "" {
			return true
		}
	}
	return false
}

type customerFieldCandidate struct {
	value string
	path  string
}

// customerIdentityField deliberately excludes generic `id` and `uuid` fields.
// Those names commonly identify memberships, sessions, or rows other than the
// customer. Ambiguous exports must use --id-field so billing identity is never
// guessed.
func customerIdentityField(record map[string]any, explicit string) (string, string, string) {
	if explicit != "" {
		value, path := customerField(record, explicit, nil)
		return value, path, ""
	}
	aliases := make(map[string]struct{}, len(customerIDAliases))
	for _, alias := range customerIDAliases {
		aliases[alias] = struct{}{}
	}
	var candidates []customerFieldCandidate
	collectCustomerIdentityFields(record, aliases, "", 0, &candidates)
	if len(candidates) == 0 {
		return "", "", ""
	}
	if len(candidates) > 1 {
		paths := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			paths = append(paths, candidate.path)
		}
		return "", "", fmt.Sprintf(
			"multiple possible stable customer ID fields (%s); choose one with --id-field",
			strings.Join(paths, ", "),
		)
	}
	return candidates[0].value, candidates[0].path, ""
}

func collectCustomerIdentityFields(record map[string]any, aliases map[string]struct{}, prefix string,
	depth int, candidates *[]customerFieldCandidate) {
	if depth > 8 {
		return
	}
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, ok := aliases[normalizeField(key)]; ok {
			if value := strings.TrimSpace(scalarString(record[key])); value != "" {
				*candidates = append(*candidates, customerFieldCandidate{
					value: value,
					path:  joinCustomerPath(prefix, key),
				})
			}
		}
	}
	for _, key := range keys {
		if nested, ok := record[key].(map[string]any); ok {
			collectCustomerIdentityFields(nested, aliases, joinCustomerPath(prefix, key), depth+1, candidates)
		}
	}
}

func findCustomerArray(record map[string]any, depth int) ([]any, bool) {
	if depth > 8 {
		return nil, false
	}
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, alias := range customerArrayAliases {
		for _, key := range keys {
			if normalizeField(key) == alias {
				if items, ok := record[key].([]any); ok {
					return items, true
				}
			}
		}
	}
	for _, key := range keys {
		if nested, ok := record[key].(map[string]any); ok {
			if items, found := findCustomerArray(nested, depth+1); found {
				return items, true
			}
		}
	}
	return nil, false
}

func customerField(record map[string]any, explicit string, aliases []string) (string, string) {
	if explicit != "" {
		value, ok := lookupCustomerPath(record, strings.Split(explicit, "."))
		if !ok {
			return "", explicit
		}
		return scalarString(value), explicit
	}
	for _, alias := range aliases {
		if value, path, ok := findCustomerField(record, alias, "", 0); ok {
			return scalarString(value), path
		}
	}
	return "", ""
}

func lookupCustomerPath(record map[string]any, path []string) (any, bool) {
	var current any = record
	for _, wanted := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		found := false
		for key, value := range object {
			if strings.EqualFold(key, wanted) || normalizeField(key) == normalizeField(wanted) {
				current, found = value, true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	return current, true
}

func findCustomerField(record map[string]any, alias, prefix string, depth int) (any, string, bool) {
	if depth > 8 {
		return nil, "", false
	}
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if normalizeField(key) == alias {
			return record[key], joinCustomerPath(prefix, key), true
		}
	}
	for _, key := range keys {
		if nested, ok := record[key].(map[string]any); ok {
			if value, path, found := findCustomerField(
				nested, alias, joinCustomerPath(prefix, key), depth+1,
			); found {
				return value, path, true
			}
		}
	}
	return nil, "", false
}

func normalizeField(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Map(func(char rune) rune {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			return char
		}
		return -1
	}, value)
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return fmt.Sprintf("%g", typed)
	case float32:
		return fmt.Sprintf("%g", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case int32:
		return fmt.Sprintf("%d", typed)
	default:
		return ""
	}
}

func joinCustomerPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func firstStrings(values []string, count int) []string {
	if len(values) <= count {
		return values
	}
	return values[:count]
}

func formatFieldCounts(values map[string]int) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	formatted := make([]string, 0, len(keys))
	for _, key := range keys {
		formatted = append(formatted, fmt.Sprintf("%s (%d)", key, values[key]))
	}
	return strings.Join(formatted, ", ")
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
