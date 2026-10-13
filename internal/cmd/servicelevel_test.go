package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/config"
	"github.com/factuarea/factuarea-cli/internal/exit"
	"github.com/factuarea/factuarea-cli/internal/output"
)

const serviceLevelTestUUID = "01900000-0000-7000-8000-000000000001"

func runServiceLevelTestCommand(t *testing.T, serverURL string, args ...string) (string, string, error) {
	return runServiceLevelTestCommandWithKey(t, serverURL, "fact_test_"+strings.Repeat("a", 24), args...)
}

func runServiceLevelTestCommandWithKey(t *testing.T, serverURL, apiKey string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("FACTUAREA_API_KEY", apiKey)
	t.Setenv("FACTUAREA_BASE_URL", serverURL)
	t.Setenv("FACTUAREA_PROFILE", "")
	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"--json", "crm", "service-level"}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestServiceLevelSevenNativeRoutesAndOriginalIntent(t *testing.T) {
	for _, op := range serviceLevelOperations {
		t.Run(op.action, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v1/account" {
					fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm:read","crm:write","service_level:read","service_level:write"]}}}`)
					return
				}
				calls++
				wantPath := strings.ReplaceAll(op.path, "{ticket}", serviceLevelTestUUID)
				if r.Method != op.method || r.URL.Path != wantPath || r.Header.Get("Authorization") != "Bearer fact_test_"+strings.Repeat("a", 24) {
					t.Errorf("request does not match native route/profile: %s %s", r.Method, r.URL.Path)
				}
				body, _ := io.ReadAll(r.Body)
				if op.mutating() {
					if r.Header.Get("Idempotency-Key") != "original-sla-intent" || string(body) != serviceLevelExamples[op.action] {
						t.Error("original idempotency key or closed body changed")
					}
					fmt.Fprintf(w, `{"data":{"id":%q,"version":2}}`, serviceLevelTestUUID)
				} else if op.paginated {
					query := r.URL.Query()
					if query.Get("page") != "2" || query.Get("per_page") != "3" || query.Has("cursor") || query.Has("filters") || len(query) != 2 {
						t.Error("page/per_page wire contract changed or unsupported filters/cursor were added")
					}
					fmt.Fprint(w, `{"data":{"items":[],"total":0,"page":2,"per_page":3}}`)
				} else {
					fmt.Fprintf(w, `{"data":{"id":%q,"version":2,"state":"on_track"}}`, serviceLevelTestUUID)
				}
			}))
			defer server.Close()
			args := []string{op.action}
			if op.ticket {
				args = append(args, serviceLevelTestUUID)
			}
			if op.mutating() {
				args = append(args, "--data", serviceLevelExamples[op.action], "--idempotency-key", "original-sla-intent")
			} else if op.paginated {
				args = append(args, "--page", "2", "--per-page", "3")
			}
			stdout, stderr, err := runServiceLevelTestCommand(t, server.URL, args...)
			if err != nil || calls != 1 || !json.Valid([]byte(stdout)) || stderr != "" {
				t.Fatalf("native operation failed: calls=%d err=%v stdout=%s stderr=%s", calls, err, stdout, stderr)
			}
		})
	}
	root := NewRootCmd()
	var manifest bytes.Buffer
	root.SetOut(&manifest)
	root.SetArgs([]string{"commands", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var entries []manifestEntry
	if err := json.Unmarshal(manifest.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Command, "factuarea crm service-level ") {
			count++
			if entry.OperationID == "" || entry.RequiredScope == "" || entry.Irreversible {
				t.Errorf("invalid native manifest entry: %+v", entry)
			}
		}
	}
	if count != 7 {
		t.Fatalf("native discovery requires exactly seven entries, got %d", count)
	}
}

func TestServiceLevelHelpAndPaginationRejectNonNativeFiltersAndCursor(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "")
	t.Setenv("FACTUAREA_PROFILE", "")
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	t.Setenv("FACTUAREA_BASE_URL", server.URL)

	for _, args := range [][]string{
		{"--json", "crm", "service-level", "--help"},
		{"--json", "crm", "service-level", "calendars", "--help"},
		{"--json", "crm", "service-level", "history", "--help"},
	} {
		root := NewRootCmd()
		var stdout, stderr bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stderr)
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatalf("help failed for %v: %v (%s)", args, err, stderr.String())
		}
		text := stdout.String()
		if strings.Contains(strings.Join(args, " "), "service-level --help") {
			for _, command := range []string{"status", "history", "calendars", "configure", "pause", "resume", "update-calendar"} {
				if !strings.Contains(text, command) {
					t.Fatalf("native command group help omitted %q: %s", command, text)
				}
			}
		}
		if !strings.Contains(text, "--page") || !strings.Contains(text, "--per-page") || !strings.Contains(text, "--all") {
			if strings.Contains(strings.Join(args, " "), "calendars") || strings.Contains(strings.Join(args, " "), "history") {
				t.Fatalf("native page help omitted pagination flags: %s", text)
			}
		}
		if strings.Contains(text, "--cursor") || strings.Contains(text, "--filters") {
			t.Fatalf("help advertised unsupported query inputs: %s", text)
		}
	}

	for _, flag := range []string{"--cursor", "--filters"} {
		_, _, err := runServiceLevelTestCommand(t, server.URL, "calendars", flag, "opaque")
		if err == nil || exit.ForError(err) != exit.Usage {
			t.Fatalf("unsupported native query flag %s was accepted: %v", flag, err)
		}
	}
	if requests != 0 {
		t.Fatalf("unsupported query input reached the API (%d requests)", requests)
	}
}

func TestServiceLevelDryRunValidatesOriginalBodyWithoutCredentialsOrHTTP(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	t.Setenv("FACTUAREA_API_KEY", "")
	t.Setenv("FACTUAREA_PROFILE", "")
	t.Setenv("FACTUAREA_BASE_URL", server.URL)

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"--json", "crm", "service-level", "configure", serviceLevelTestUUID,
		"--data", serviceLevelExamples["configure"], "--dry-run"})
	if err := root.Execute(); err != nil {
		t.Fatalf("dry-run resolved credentials or rejected the native body: %v (%s)", err, stderr.String())
	}
	if stdout.String() != serviceLevelExamples["configure"]+"\n" || requests != 0 {
		t.Fatalf("dry-run rewrote the original JSON or performed I/O: requests=%d stdout=%q", requests, stdout.String())
	}
}

func TestServiceLevelCAS409PreservesConflictAndRedactsCredential(t *testing.T) {
	apiKey := "fact_test_" + strings.Repeat("c", 24)
	var writes int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/account" {
			fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm:write","service_level:write"]}}}`)
			return
		}
		writes++
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/crm/service-tickets/"+serviceLevelTestUUID+"/sla/resume" ||
			r.Header.Get("Idempotency-Key") != "original-sla-cas-attempt" || string(body) != serviceLevelExamples["resume"] {
			t.Error("CAS request changed its owner path, original key or exact body")
		}
		w.Header().Set("X-Request-Id", apiKey)
		w.Header().Set("Retry-After", apiKey)
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintf(w, `{"error":{"type":"owner_conflict","code":"version_conflict","message":"stale %s","details":{"field_errors":{"expected_version":["stale"]},"diagnostic":"%s"}},"errors":{"cycle_id":["%s"]}}`, apiKey, apiKey, apiKey)
	}))
	defer server.Close()

	stdout, stderr, err := runServiceLevelTestCommandWithKey(t, server.URL, apiKey,
		"resume", serviceLevelTestUUID, "--data", serviceLevelExamples["resume"], "--idempotency-key", "original-sla-cas-attempt")
	if err == nil || exit.ForError(err) != exit.Conflict || writes != 1 || stdout != "" || !json.Valid([]byte(stderr)) {
		t.Fatalf("CAS 409 was retried, confirmed or remapped: writes=%d err=%v stdout=%q stderr=%q", writes, err, stdout, stderr)
	}
	for _, field := range []string{"version_conflict", "expected_version", `"http_status":409`, `"retry_after":"[REDACTED]"`, `"request_id":"[REDACTED]"`, `"diagnostic":"[REDACTED]"`} {
		if !strings.Contains(stderr, field) {
			t.Fatalf("CAS response lost native/redacted field %q: %s", field, stderr)
		}
	}
	if strings.Contains(stdout+stderr, apiKey) {
		t.Fatal("CAS error disclosed the actual bearer credential")
	}
}

func TestServiceLevelSelectedSandboxProfilesKeepCompanyReadsIsolated(t *testing.T) {
	type fixture struct{ profile, key, company, ticket string }
	fixtures := []fixture{
		{"service-level-sandbox-a", "fact_test_" + strings.Repeat("a", 24), "01900000-0000-7000-8000-000000000011", "01900000-0000-7000-8000-000000000021"},
		{"service-level-sandbox-b", "fact_test_" + strings.Repeat("b", 24), "01900000-0000-7000-8000-000000000012", "01900000-0000-7000-8000-000000000022"},
	}
	genericKey := "fact_test_" + strings.Repeat("c", 24)
	byKey := map[string]fixture{}
	for _, item := range fixtures {
		byKey[item.key] = item
	}
	var requestsMu sync.Mutex
	requestsByKey := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		item, ok := byKey[key]
		if !ok {
			http.Error(w, `{"error":{"message":"unknown credential"}}`, http.StatusUnauthorized)
			return
		}
		requestsMu.Lock()
		requestsByKey[key]++
		requestsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/account" {
			fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm:read","service_level:read"]}}}`)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/v1/crm/service-tickets/"+item.ticket+"/sla" || len(r.URL.Query()) != 0 {
			http.Error(w, `{"error":{"message":"unexpected route or tenant override"}}`, http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, `{"data":{"id":%q,"company_id":%q,"version":1,"state":"on_track"}}`, item.ticket, item.company)
	}))
	defer server.Close()

	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(configHome, "missing-session-bus"))
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "unix:path="+filepath.Join(configHome, "missing-system-bus"))
	t.Setenv("FACTUAREA_API_KEY", genericKey)
	t.Setenv("FACTUAREA_PROFILE", "")
	t.Setenv("FACTUAREA_BASE_URL", server.URL)
	store := config.NewFileStore(filepath.Join(configHome, "factuarea", "config.toml"))
	for _, item := range fixtures {
		if err := store.SetKey(item.profile, item.key); err != nil {
			t.Fatalf("cannot prepare isolated profile fixture: %v", err)
		}
	}

	for _, item := range fixtures {
		t.Run(item.profile, func(t *testing.T) {
			root := NewRootCmd()
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs([]string{"--json", "--profile", item.profile, "crm", "service-level", "status", item.ticket})
			if err := root.Execute(); err != nil {
				t.Fatalf("selected tenant read failed: %v; stderr=%s", err, stderr.String())
			}
			if !strings.Contains(stdout.String(), item.company) || !strings.Contains(stdout.String(), item.ticket) {
				t.Fatalf("profile %q returned another company's fixture: %s", item.profile, stdout.String())
			}
			for _, other := range fixtures {
				if other.key != item.key && (strings.Contains(stdout.String(), other.company) || strings.Contains(stdout.String(), other.ticket)) {
					t.Fatalf("profile %q exposed tenant %s: %s", item.profile, other.company, stdout.String())
				}
			}
			if strings.Contains(stdout.String()+stderr.String(), item.key) || strings.Contains(stdout.String()+stderr.String(), genericKey) {
				t.Fatal("tenant response disclosed a profile or fallback credential")
			}
		})
	}

	requestsMu.Lock()
	defer requestsMu.Unlock()
	for _, item := range fixtures {
		if requestsByKey[item.key] != 2 {
			t.Errorf("profile %q used %d requests; want account preflight and one native read", item.profile, requestsByKey[item.key])
		}
	}
	if requestsByKey[genericKey] != 0 {
		t.Fatalf("selected profiles fell back to the generic credential for %d requests", requestsByKey[genericKey])
	}
}

func TestServiceLevelRejectsAuthorityMissingNullAndCoercedTypes(t *testing.T) {
	cases := []struct{ action, body string }{
		{"configure", strings.Replace(serviceLevelExamples["configure"], `"cycle_id":null,`, "", 1)},
		{"configure", strings.Replace(serviceLevelExamples["configure"], `"expected_version":null`, `"expected_version":"1"`, 1)},
		{"configure", strings.Replace(serviceLevelExamples["configure"], `"policy_version":1`, `"policy_version":2`, 1)},
		{"configure", strings.Replace(serviceLevelExamples["configure"], `"calendar_version":null`, `"calendar_version":1`, 1)},
		{"update-calendar", strings.Replace(serviceLevelExamples["update-calendar"], `"id":null`, `"id":"01900000-0000-4000-8000-000000000001"`, 1)},
		{"pause", strings.Replace(serviceLevelExamples["pause"], `"reason":"approval"`, `"reason":"waiting_customer"`, 1)},
		{"pause", strings.ReplaceAll(serviceLevelExamples["pause"], ":true", ":false")},
		{"resume", strings.Replace(serviceLevelExamples["resume"], `"expected_version":2`, `"expected_version":2.0`, 1)},
		{"resume", strings.Replace(serviceLevelExamples["resume"], `"reason":"approval"`, `"reason":"approval","company_id":7`, 1)},
		{"update-calendar", strings.Replace(serviceLevelExamples["update-calendar"], `"holidays":[]`, `"holidays":{}`, 1)},
	}
	for index, test := range cases {
		if err := validateServiceLevelBody(test.action, []byte(test.body)); err == nil {
			t.Errorf("invalid closed JSON %d accepted", index)
		}
	}
	for _, key := range []string{"", "bad\nkey", strings.Repeat("x", 256)} {
		if validateServiceLevelIdempotencyKey(key) == nil {
			t.Error("invalid original intent key accepted")
		}
	}
}

func TestServiceLevelAmbiguousWritesNeverRetryOrInventReceipt(t *testing.T) {
	for _, mode := range []string{"timeout", "503", "redirect", "queued"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				switch mode {
				case "timeout":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
				case "503":
					w.WriteHeader(http.StatusServiceUnavailable)
					fmt.Fprint(w, `{"error":{"type":"api_error","code":"internal_error","message":"Productor no disponible"}}`)
				case "redirect":
					w.Header().Set("Location", "/must-not-dispatch")
					w.WriteHeader(http.StatusTemporaryRedirect)
				default:
					fmt.Fprint(w, `{"data":{"status":"queued"}}`)
				}
			}))
			defer server.Close()
			stdout, stderr, err := runServiceLevelTestCommand(t, server.URL, "resume", serviceLevelTestUUID,
				"--data", serviceLevelExamples["resume"], "--idempotency-key", "original-sla-intent", "--skip-scope-check")
			if err == nil || calls != 1 || stdout != "" || !json.Valid([]byte(stderr)) {
				t.Fatalf("ambiguous write was replayed/confirmed: calls=%d err=%v stdout=%s stderr=%s", calls, err, stdout, stderr)
			}
			if (mode == "timeout" || mode == "queued") && exit.ForError(err) != exit.Network {
				t.Fatalf("unconfirmed result must retain transport failure, got %d", exit.ForError(err))
			}
		})
	}
}

func TestServiceLevelPageIterationAndLimitAreExplicit(t *testing.T) {
	for _, limit := range []string{"1", "2"} {
		t.Run(limit, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if r.Method != http.MethodGet || r.URL.Path != "/v1/crm/service-calendars" || r.URL.Query().Get("per_page") != "1" {
					t.Error("wrong native page request")
				}
				fmt.Fprintf(w, `{"data":{"items":[{"id":%q,"version":1}],"total":2,"page":%s,"per_page":1}}`, serviceLevelTestUUID, r.URL.Query().Get("page"))
			}))
			defer server.Close()
			stdout, _, err := runServiceLevelTestCommand(t, server.URL, "calendars", "--all", "--per-page", "1", "--max-pages", limit, "--skip-scope-check")
			if limit == "1" && (err == nil || calls != 1 || !strings.Contains(err.Error(), "parcial")) {
				t.Fatal("limit must retain partial output and stop explicitly")
			}
			if limit == "2" && (err != nil || calls != 2) {
				t.Fatalf("page iteration failed: %v (%d requests)", err, calls)
			}
			if strings.Count(stdout, "\n") != calls {
				t.Error("NDJSON must preserve every received native item")
			}
		})
	}
}

func TestServiceLevelTablePreservesUUIDAndSanitizesTerminal(t *testing.T) {
	body := []byte(fmt.Sprintf(`{"data":{"items":[{"id":%q,"version":2,"name":"Servicio\u001b[31m\nOtro","timezone":"UTC","mode":"business"}],"total":1,"page":1,"per_page":50}}`, serviceLevelTestUUID))
	var table, raw bytes.Buffer
	op := serviceLevelOperations[2]
	if err := printServiceLevelBody(&table, body, output.Human, op); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(table.String(), serviceLevelTestUUID) || strings.ContainsRune(table.String(), '\x1b') || !strings.Contains(table.String(), "Página 1") {
		t.Fatal("table changed public UUID, injected terminal escapes or lost page")
	}
	if err := printServiceLevelBody(&raw, body, output.JSON, op); err != nil || raw.String() != string(body)+"\n" {
		t.Fatal("JSON must preserve native response unchanged")
	}
}
