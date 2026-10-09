package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
	"github.com/factuarea/factuarea-cli/internal/output"
)

const serviceLevelTestUUID = "01900000-0000-7000-8000-000000000001"

func runServiceLevelTestCommand(t *testing.T, serverURL string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("FACTUAREA_API_KEY", "fact_test_"+strings.Repeat("a", 24))
	t.Setenv("FACTUAREA_BASE_URL", serverURL)
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
					if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "3" || r.URL.Query().Has("cursor") {
						t.Error("page/per_page wire contract changed")
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
