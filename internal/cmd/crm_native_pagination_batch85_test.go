package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/spec"
)

// Client transport fixtures prove the published paging protocol, not native
// issuer/tenant admission or backend effects. All IDs come from the embedded
// real export. The three opaque insight schemas are qualified by the actual
// CrmForecastWire source, whose published body is preserved whole.
func crmNativePagingOps(t *testing.T) []genOp {
	t.Helper()
	var ops []genOp
	for _, op := range generatedOps() {
		if op.Method != http.MethodGet || !strings.HasPrefix(op.Path, "/crm/") {
			continue
		}
		for _, parameter := range op.QueryParams {
			if parameter.Name == "cursor" || parameter.Name == "page" {
				ops = append(ops, op)
				break
			}
		}
	}
	if len(ops) != 42 {
		t.Fatalf("actual paged CRM GETs=%d", len(ops))
	}
	return ops
}

func crmNativeFixtureSet(object map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	for _, key := range parts[:len(parts)-1] {
		child, ok := object[key].(map[string]any)
		if !ok {
			child = map[string]any{}
			object[key] = child
		}
		object = child
	}
	object[parts[len(parts)-1]] = value
}

func crmNativeFixturePage(t *testing.T, plan crmNativePagination, page int, more bool) []byte {
	t.Helper()
	object := map[string]any{}
	// UseNumber-compatible RawMessage retains an exact decimal, null and Unicode.
	rows := []json.RawMessage{json.RawMessage(`{"id":"01900000-0000-7000-8000-000000000001","amount":9000000000000000.0001,"optional":null,"name":"Àngela 山"}`)}
	crmNativeFixtureSet(object, plan.Items, rows)
	if plan.Parameter == "cursor" {
		var next any
		if more {
			next = "native opaque +/= cursor"
		}
		crmNativeFixtureSet(object, plan.Cursor, next)
		if plan.HasMore != "" {
			crmNativeFixtureSet(object, plan.HasMore, more)
		}
	} else {
		size := plan.FixedPerPage
		if size == 0 {
			size = 1
			crmNativeFixtureSet(object, plan.PerPage, size)
		}
		if more && page == 1 && plan.FixedPerPage > 0 {
			for len(rows) < int(size) {
				rows = append(rows, json.RawMessage(fmt.Sprintf(`{"id":"01900000-0000-7000-8000-%012x","amount":9000000000000000.0001,"optional":null,"name":"Àngela 山"}`, len(rows)+1)))
			}
			crmNativeFixtureSet(object, plan.Items, rows)
		}
		crmNativeFixtureSet(object, plan.Page, page)
		total := size + 1
		if !more && page == 1 {
			total = 1
		}
		crmNativeFixtureSet(object, plan.Total, total)
		if plan.LastPage != "" {
			last := 2
			if !more && page == 1 {
				last = 1
			}
			crmNativeFixtureSet(object, plan.LastPage, last)
		}
	}
	body, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func crmNativeSchemaResolve(t *testing.T, root map[string]any, schema map[string]any) map[string]any {
	t.Helper()
	for depth := 0; depth < 16; depth++ {
		ref, ok := schema["$ref"].(string)
		if !ok {
			return schema
		}
		var value any = root
		for _, key := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			object, ok := value.(map[string]any)
			if !ok {
				t.Fatal("actual schema ref is not an object")
			}
			value = object[key]
		}
		var valid bool
		schema, valid = value.(map[string]any)
		if !valid {
			t.Fatalf("actual schema ref unavailable: %s", ref)
		}
	}
	t.Fatal("actual schema ref depth exceeded")
	return nil
}

func crmNativeSchemaField(t *testing.T, root map[string]any, schema map[string]any, path string) map[string]any {
	t.Helper()
	for _, key := range strings.Split(path, ".") {
		schema = crmNativeSchemaResolve(t, root, schema)
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("schema lacks properties at %s", path)
		}
		schema, ok = properties[key].(map[string]any)
		if !ok {
			t.Fatalf("schema lacks actual field %s", path)
		}
	}
	return crmNativeSchemaResolve(t, root, schema)
}

func TestCRMNativePaginationBatch85EveryActualPagedGETUsesItsPublishedEnvelopeAndBoundedFlags(t *testing.T) {
	var schema map[string]any
	if json.Unmarshal(spec.Raw, &schema) != nil {
		t.Fatal("real embedded spec invalid")
	}
	paths := schema["paths"].(map[string]any)
	root := NewRootCmd()
	for _, op := range crmNativePagingOps(t) {
		plan, known := crmNativePaginationFor(op)
		if !known || plan.Path != op.Path {
			t.Fatalf("actual pagination absent: %s", op.OperationID)
		}
		command, _, err := root.Find(append(append([]string{}, op.Groups...), op.Action))
		if err != nil || command.Flags().Lookup("all") == nil || command.Flags().Lookup("max-pages") == nil || command.Flags().Lookup(plan.Parameter) == nil || op.isMutating() {
			t.Fatalf("native bounded read missing: %s", op.OperationID)
		}
		exported := paths[op.Path].(map[string]any)["get"].(map[string]any)
		body := exported["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		if strings.HasPrefix(op.OperationID, "public-api.v1.crm-insights.") {
			continue
		} // actual native CrmForecastWire source, not an invented schema
		if crmNativeSchemaField(t, schema, body, plan.Items)["type"] != "array" {
			t.Fatalf("native row array changed: %s", op.OperationID)
		}
		if plan.Parameter == "cursor" {
			crmNativeSchemaField(t, schema, body, plan.Cursor)
		} else {
			crmNativeSchemaField(t, schema, body, plan.Page)
			crmNativeSchemaField(t, schema, body, plan.Total)
			if plan.PerPage != "" {
				crmNativeSchemaField(t, schema, body, plan.PerPage)
			}
			if plan.LastPage != "" {
				crmNativeSchemaField(t, schema, body, plan.LastPage)
			}
		}
	}
	if len(crmNativePaginationContracts) != 42 {
		t.Fatal("closed paging catalogue changed")
	}
}

func crmNativePagingArgs(op genOp) ([]string, url.Values) {
	args := append([]string{"--json"}, op.Groups...)
	args = append(args, op.Action)
	for range op.PathParams {
		args = append(args, "01900000-0000-7000-8000-000000000001")
	}
	retained := url.Values{}
	for _, p := range op.QueryParams {
		if p.Name == "cursor" || p.Name == "page" {
			continue
		}
		if p.Required || p.Name == "search" || p.Name == "filters" || p.Name == "limit" || p.Name == "per_page" {
			value := "retained native option"
			switch p.Name {
			case "limit", "per_page":
				value = "1"
			case "filters", "filtersJson":
				value = "{}"
			case "entity_id", "run_id", "source_blob_id":
				value = "01900000-0000-7000-8000-000000000001"
			case "entity_type":
				value = "lead"
			case "kind":
				value = "person"
			case "duplicate_mode":
				value = "skip"
			case "purpose":
				value = "contact_request"
			case "document_type":
				value = "quote"
			case "period":
				value = "2026-10"
			case "timezone":
				value = "Europe/Madrid"
			case "start":
				value = "2026-10-01"
			case "end":
				value = "2026-10-10"
			case "clock_mode":
				value = "elapsed"
			case "as_of":
				value = "2026-10-10T10:00:00Z"
			case "predicate_hash":
				value = strings.Repeat("a", 64)
			case "metric":
				value = "backlog"
			case "thresholdDays":
				value = "30"
			}
			args = append(args, "--"+p.Name, value)
			retained.Set(p.Name, value)
		}
	}
	return append(args, "--all", "--max-pages", "2"), retained
}

func TestCRMNativePaginationBatch85All42TraverseExactlyTwoNativePagesKeepingCredentialFiltersAndWireValues(t *testing.T) {
	for _, op := range crmNativePagingOps(t) {
		t.Run(op.OperationID, func(t *testing.T) {
			plan, _ := crmNativePaginationFor(op)
			calls := 0
			args, retained := crmNativePagingArgs(op)
			pathArgs := make([]string, len(op.PathParams))
			for i := range pathArgs {
				pathArgs[i] = "01900000-0000-7000-8000-000000000001"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v1/account" {
					fmt.Fprintf(w, `{"data":{"api_key":{"scopes":[%q]}}}`, op.RequiredScope)
					return
				}
				calls++
				if r.Method != http.MethodGet || r.URL.Path != op.buildPath(pathArgs) || r.ContentLength != 0 || r.Header.Get("Authorization") != "Bearer fact_test_"+strings.Repeat("a", 24) {
					t.Error("native page changed current credential/method/path/body")
				}
				for key, value := range retained {
					if r.URL.Query().Get(key) != value[0] {
						t.Errorf("retained option changed: %s", key)
					}
				}
				if plan.Parameter == "cursor" {
					expected := ""
					if calls == 2 {
						expected = "native opaque +/= cursor"
					}
					if r.URL.Query().Get("cursor") != expected {
						t.Error("opaque native cursor was inferred or changed")
					}
				} else {
					expected := ""
					if calls == 2 {
						expected = "2"
					}
					if r.URL.Query().Get("page") != expected {
						t.Error("native numeric page changed")
					}
				}
				if calls > 2 {
					t.Error("native pagination prefetched beyond confirmed end")
				}
				_, _ = w.Write(crmNativeFixturePage(t, plan, calls, calls == 1))
			}))
			t.Cleanup(server.Close)
			crmDeliveryEnv(t, server.URL)
			root := NewRootCmd()
			root.SetArgs(args)
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			if err := root.Execute(); err != nil || calls != 2 {
				t.Fatalf("native all failed: calls=%d err=%v", calls, err)
			}
			scanner := bufio.NewScanner(&out)
			lines := 0
			for scanner.Scan() {
				lines++
				if !json.Valid(scanner.Bytes()) || !bytes.Contains(scanner.Bytes(), []byte("9000000000000000.0001")) || !bytes.Contains(scanner.Bytes(), []byte(`"optional":null`)) {
					t.Fatal("native page values reconstructed")
				}
			}
			if scanner.Err() != nil || lines != 2 {
				t.Fatal("all output is not two bounded JSON records")
			}
		})
	}
}

func TestCRMNativePaginationBatch85MalformedMetadataRepeatedCursorAndBudgetNeverFetchAnExtraPage(t *testing.T) {
	for _, scenario := range []string{"missing_cursor", "empty_cursor", "repeated_cursor", "page_mismatch", "missing_total", "null_total", "null_rows", "inconsistent_has_more", "budget_cursor", "budget_page"} {
		t.Run(scenario, func(t *testing.T) {
			id := "listBookings"
			if strings.Contains(scenario, "page") || scenario == "missing_total" || scenario == "null_total" {
				id = "crmListCommercialDocuments"
			}
			if scenario == "inconsistent_has_more" {
				id = "crmListSalesTeams"
			}
			var op genOp
			for _, candidate := range generatedOps() {
				if candidate.OperationID == id {
					op = candidate
				}
			}
			plan, _ := crmNativePaginationFor(op)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v1/account" {
					fmt.Fprintf(w, `{"data":{"api_key":{"scopes":[%q]}}}`, op.RequiredScope)
					return
				}
				calls++
				var body map[string]any
				_ = json.Unmarshal(crmNativeFixturePage(t, plan, calls, true), &body)
				switch scenario {
				case "missing_cursor":
					delete(body["data"].(map[string]any), "next_cursor")
				case "empty_cursor":
					crmNativeFixtureSet(body, plan.Cursor, "")
				case "page_mismatch":
					crmNativeFixtureSet(body, plan.Page, 9)
				case "missing_total":
					delete(body["data"].(map[string]any), "total")
				case "null_total":
					crmNativeFixtureSet(body, plan.Total, nil)
				case "inconsistent_has_more":
					crmNativeFixtureSet(body, plan.HasMore, false)
				case "null_rows":
					crmNativeFixtureSet(body, plan.Items, nil)
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			t.Cleanup(server.Close)
			crmDeliveryEnv(t, server.URL)
			args, _ := crmNativePagingArgs(op)
			if strings.HasPrefix(scenario, "budget_") {
				args[len(args)-1] = "1"
			}
			root := NewRootCmd()
			root.SetArgs(args)
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			err := root.Execute()
			if err == nil {
				t.Fatal("invalid/incomplete page asserted completion")
			}
			expected := 1
			if scenario == "repeated_cursor" {
				expected = 2
			}
			if calls != expected {
				t.Fatalf("extra native page: %d", calls)
			}
			if strings.HasPrefix(scenario, "budget_") {
				var api *apierr.APIError
				if !errors.As(err, &api) || api.Code != "pagination_budget_exhausted" {
					t.Fatalf("actual budget error lost: %v", err)
				}
				details := api.Details.(map[string]any)
				if details["partial"] != true || details["next_"+plan.Parameter] == nil {
					t.Fatal("actual continuation lost")
				}
			}
		})
	}
}

func TestCRMNativePaginationBatch85InvalidAllInputFailsBeforeCredentialOrHTTPAndLegacyStaysUnchanged(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "")
	t.Setenv("FACTUAREA_PROFILE", "")
	for _, flags := range [][]string{{"--max-pages", "0"}, {"--max-pages", "1001"}, {"--page", "0"}, {"--page", "1.5"}, {"--page", "01"}} {
		root := NewRootCmd()
		root.SetArgs(append([]string{"crm-documents", "list", "--all"}, flags...))
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		var usage *apierr.UsageError
		if err := root.Execute(); !errors.As(err, &usage) {
			t.Fatalf("invalid native input reached credentials: %v", err)
		}
	}
	legacy := genOp{OperationID: "public-api.v1.products.list", Method: http.MethodGet, Path: "/products", Groups: []string{"products"}, Action: "list", QueryParams: []genParam{{Name: "starting_after", Type: "string"}}}
	command := buildGeneratedCommand(legacy)
	if !legacy.isPaginated() || command.Flags().Lookup("paginate") == nil || command.Flags().Lookup("all") != nil {
		t.Fatal("legacy paginator changed")
	}
}

func TestCRMNativePaginationBatch85SourcePreviewAllPreservesWholeCurrentEnvelopeWithoutCreatingARun(t *testing.T) {
	var op genOp
	for _, candidate := range generatedOps() {
		if candidate.OperationID == "public-api.v1.crm-bottle-migrations.source-preview" {
			op = candidate
		}
	}
	if op.OperationID == "" {
		t.Fatal("actual read-only source preview absent")
	}
	wire := []byte(" \n{\"data\":{\"items\":[],\"next_cursor\":null,\"configuration\":null,\"profiles\":[],\"preflight_only\":true},\"meta\":{\"authorization_version\":\"current-native-c7\"}}\n")
	var compact bytes.Buffer
	if json.Compact(&compact, wire) != nil {
		t.Fatal("fixture wire invalid")
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/account" {
			fmt.Fprintf(w, `{"data":{"api_key":{"scopes":[%q]}}}`, op.RequiredScope)
			return
		}
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/v1/crm/bottle-migrations/source-preview" || r.ContentLength != 0 {
			t.Error("read-only source pagination staged or wrote an import")
		}
		_, _ = w.Write(wire)
	}))
	t.Cleanup(server.Close)
	crmDeliveryEnv(t, server.URL)
	args, _ := crmNativePagingArgs(op)
	root := NewRootCmd()
	root.SetArgs(args)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil || calls != 1 || out.String() != compact.String()+"\n" {
		t.Fatalf("source preview envelope changed or wrote: calls=%d err=%v", calls, err)
	}
}
