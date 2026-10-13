package cmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const crmInstalled82A = "01900000-0000-7000-8000-000000000001"
const crmInstalled82B = "01900000-0000-7000-8000-000000000002"
const crmInstalled82KeyA = "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa"
const crmInstalled82KeyB = "fact_test_bbbbbbbbbbbbbbbbbbbbbbbb"

type crmInstalled82Result struct {
	stdout, stderr string
	code           int
}

func crmInstalled82Environment(t *testing.T, base string) []string {
	t.Helper()
	private := t.TempDir()
	var env []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "FACTUAREA_") || name == "XDG_CONFIG_HOME" || name == "XDG_CACHE_HOME" || name == "DBUS_SESSION_BUS_ADDRESS" || name == "DBUS_SYSTEM_BUS_ADDRESS" || name == "SHELL" || name == "ENV" || name == "BASH_ENV" {
			continue
		}
		env = append(env, entry)
	}
	// A nonexistent private D-Bus socket makes the production binary use its
	// native file fallback, without probing or modifying the user's keyring.
	return append(env, "SHELL=/bin/sh", "XDG_CONFIG_HOME="+filepath.Join(private, "config"), "XDG_CACHE_HOME="+filepath.Join(private, "cache"),
		"DBUS_SESSION_BUS_ADDRESS=unix:path="+filepath.Join(private, "no-session-bus"), "DBUS_SYSTEM_BUS_ADDRESS=unix:path="+filepath.Join(private, "no-system-bus"), "FACTUAREA_BASE_URL="+base, "FACTUAREA_API_KEY="+crmInstalled82KeyA)
}

func crmInstalled82NoCredential(env []string) []string {
	var result []string
	for _, entry := range env {
		if !strings.HasPrefix(entry, "FACTUAREA_API_KEY=") {
			result = append(result, entry)
		}
	}
	return result
}

func crmInstalled82Run(t *testing.T, binary string, env []string, stdin string, args ...string) crmInstalled82Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	process := exec.CommandContext(ctx, binary, args...)
	process.Env = env
	process.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	err := process.Run()
	result := crmInstalled82Result{stdout: stdout.String(), stderr: stderr.String()}
	if err != nil {
		failure, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("actual installed binary failed to start: %v", err)
		}
		result.code = failure.ExitCode()
	}
	if ctx.Err() != nil {
		t.Fatal("installed command exceeded the bounded fixture timeout")
	}
	return result
}

func crmInstalled82Account(writer http.ResponseWriter, request *http.Request, scopes []string) {
	company := crmInstalled82A
	if request.Header.Get("Authorization") == "Bearer "+crmInstalled82KeyB {
		company = crmInstalled82B
	}
	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(map[string]any{"data": map[string]any{"company_id": company, "api_key": map[string]any{"scopes": scopes}}})
}

func crmInstalled82AssertNoBearer(t *testing.T, result crmInstalled82Result) {
	t.Helper()
	if strings.Contains(result.stdout+result.stderr, crmInstalled82KeyA) || strings.Contains(result.stdout+result.stderr, crmInstalled82KeyB) {
		t.Fatal("installed output disclosed the request bearer")
	}
}

// HTTP fixtures prove command/transport consumption, not native application ACL,
// C7, entitlements, OAuth lineage or database tenant isolation. Only the built
// production entrypoint executes; no command mocks or alternate CLI are used.
func TestCRMInstalledBatch82(t *testing.T) {
	binary := os.Getenv("FACTUAREA_CRM_INSTALLED_BIN")
	if binary == "" {
		t.Skip("run scripts/verify-crm-installed-batch82.sh to build/install the private production binary")
	}
	info, err := os.Stat(binary)
	if err != nil || info.IsDir() || info.Mode().Perm()&0111 == 0 {
		t.Fatal("private installed binary is not executable")
	}

	t.Run("help_is_available_without_credentials_or_HTTP", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
		defer server.Close()
		env := crmInstalled82NoCredential(crmInstalled82Environment(t, server.URL))
		result := crmInstalled82Run(t, binary, env, "", "--help")
		if result.code != 0 || calls.Load() != 0 {
			t.Fatal("help required a request")
		}
		for _, resource := range []string{"crm-leads", "crm-opportunities", "crm-activities", "completion", "commands", "--profile", "--json"} {
			if !strings.Contains(result.stdout, resource) {
				t.Fatalf("help lacks %s", resource)
			}
		}
		crmInstalled82AssertNoBearer(t, result)
	})
	t.Run("core_help_retains_current_scopes_CAS_and_original_flags", func(t *testing.T) {
		env := crmInstalled82NoCredential(crmInstalled82Environment(t, "http://127.0.0.1:1"))
		for _, args := range [][]string{{"crm-leads", "create", "--help"}, {"crm-opportunities", "list", "--help"}, {"crm-opportunities", "move-stage", "--help"}, {"crm", "originals", "lead-create", "--help"}} {
			result := crmInstalled82Run(t, binary, env, "", args...)
			if result.code != 0 {
				t.Fatal("core help failed")
			}
			if args[0] == "crm-leads" && (!strings.Contains(result.stdout, "crm_leads:write") || !strings.Contains(result.stdout, "first-body-output")) {
				t.Fatal("current create contract missing")
			}
			if args[0] == "crm-opportunities" && args[1] == "list" {
				for _, flag := range []string{"--filters", "--cursor", "--limit", "--all", "--max-pages", "opaco"} {
					if !strings.Contains(strings.ToLower(result.stdout), strings.ToLower(flag)) {
						t.Fatalf("native list help missing %q", flag)
					}
				}
			}
			if args[0] == "crm" && (!strings.Contains(result.stdout, "original-body-sha256") || !strings.Contains(result.stdout, "first-body-file")) {
				t.Fatal("original identity options missing")
			}
		}
	})
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run("completion_"+shell+"_requires_no_HTTP", func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
			defer server.Close()
			result := crmInstalled82Run(t, binary, crmInstalled82NoCredential(crmInstalled82Environment(t, server.URL)), "", "completion", shell)
			if result.code != 0 || result.stdout == "" || calls.Load() != 0 || !strings.Contains(strings.ToLower(result.stdout), "factuarea") {
				t.Fatal("completion not produced by actual registered tree")
			}
			crmInstalled82AssertNoBearer(t, result)
		})
	}
	t.Run("flag_completion_does_not_download_accounts", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
		defer server.Close()
		result := crmInstalled82Run(t, binary, crmInstalled82NoCredential(crmInstalled82Environment(t, server.URL)), "", "__complete", "crm-leads", "create", "--first")
		if result.code != 0 || calls.Load() != 0 || !strings.Contains(result.stdout, "--first-body-output") {
			t.Fatal("native flag completion missing or made a request")
		}
	})
	t.Run("manifest_uses_existing_operation_ids_once", func(t *testing.T) {
		result := crmInstalled82Run(t, binary, crmInstalled82NoCredential(crmInstalled82Environment(t, "http://127.0.0.1:1")), "", "--json", "commands")
		var entries []manifestEntry
		if result.code != 0 || json.Unmarshal([]byte(result.stdout), &entries) != nil {
			t.Fatal("installed manifest is not JSON")
		}
		for _, op := range crmCoreNativeOps() {
			count := 0
			for _, entry := range entries {
				if entry.OperationID == op.OperationID {
					count++
					if entry.RequiredScope != op.RequiredScope {
						t.Fatal("installed scope differs from Source")
					}
				}
			}
			if count != 1 {
				t.Fatalf("Source operation %s count=%d", op.OperationID, count)
			}
		}
		crmInstalled82AssertNoBearer(t, result)
	})
	t.Run("unknown_flags_and_invalid_UUID_exit2_before_HTTP", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
		defer server.Close()
		env := crmInstalled82Environment(t, server.URL)
		for _, args := range [][]string{{"--json", "crm-leads", "create", "--company-id", crmInstalled82B}, {"--json", "crm-opportunities", "list", "--all", "--max-pages", "0"}} {
			result := crmInstalled82Run(t, binary, env, "", args...)
			if result.code != 2 || result.stdout != "" || calls.Load() != 0 || !json.Valid([]byte(result.stderr)) {
				t.Fatal("local closed flags did not fail before dispatch")
			}
		}
		for _, id := range []string{
			"not-a-uuid",
			"01900000-0000-4000-8000-000000000001", // UUIDv4
			"01900000-0000-7000-7000-000000000001", // invalid RFC variant
			"../" + crmInstalled82A,
		} {
			result := crmInstalled82Run(t, binary, env, "", "--json", "crm-opportunities", "show", id)
			if result.code != 2 || result.stdout != "" || calls.Load() != 0 || !json.Valid([]byte(result.stderr)) {
				t.Fatalf("installed CLI accepted or dispatched invalid UUID %q", id)
			}
			crmInstalled82AssertNoBearer(t, result)
		}
	})
	t.Run("dry_run_preserves_decimal_null_without_HTTP_or_private_file", func(t *testing.T) {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
		defer server.Close()
		first := filepath.Join(t.TempDir(), "not-created.json")
		result := crmInstalled82Run(t, binary, crmInstalled82NoCredential(crmInstalled82Environment(t, server.URL)), "", "crm-opportunities", "create", "--dry-run", "--first-body-output", first, "-d", ` {"amount":"9000000000000000.0001","optional":null,"title":"Preview"} `)
		if result.code != 0 || calls.Load() != 0 || !strings.Contains(result.stdout, `"9000000000000000.0001"`) || !strings.Contains(result.stdout, `"optional":null`) {
			t.Fatal("installed dry run changed the wire or dispatched")
		}
		if _, err := os.Stat(first); !os.IsNotExist(err) {
			t.Fatal("dry run persisted an original attempt")
		}
	})
	t.Run("native_login_and_selected_profiles_remain_isolated", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/account" {
				crmInstalled82Account(w, r, []string{"crm_opportunities:read"})
				return
			}
			if r.Method != http.MethodGet || r.URL.Path != "/v1/crm/opportunity" ||
				r.URL.Query().Get("filters") != `{"status":"open"}` || r.URL.Query().Has("company_id") {
				t.Errorf("unexpected sandbox CRM read: %s %s", r.Method, r.URL.String())
				http.Error(w, "unexpected request", http.StatusBadRequest)
				return
			}
			company, opportunity := crmInstalled82A, "01900000-0000-7000-8000-000000000011"
			if r.Header.Get("Authorization") == "Bearer "+crmInstalled82KeyB {
				company, opportunity = crmInstalled82B, "01900000-0000-7000-8000-000000000022"
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"items":       []map[string]string{{"id": opportunity, "company_id": company, "status": "open"}},
				"next_cursor": nil,
			}})
		}))
		defer server.Close()
		env := crmInstalled82Environment(t, server.URL)
		for _, profile := range []struct {
			name, key, company, opportunityID string
		}{
			{"batch82-a", crmInstalled82KeyA, crmInstalled82A, "01900000-0000-7000-8000-000000000011"},
			{"batch82-b", crmInstalled82KeyB, crmInstalled82B, "01900000-0000-7000-8000-000000000022"},
		} {
			login := crmInstalled82Run(t, binary, env, profile.key+"\n", "--profile", profile.name, "--no-input", "login", "--api-key", "-")
			if login.code != 0 {
				t.Fatal("native login failed")
			}
			crmInstalled82AssertNoBearer(t, login)
			read := crmInstalled82Run(t, binary, env, "", "--profile", profile.name, "--json", "whoami")
			if read.code != 0 || !strings.Contains(read.stdout, profile.company) {
				t.Fatal("selected profile crossed to the generic environment key")
			}
			crmInstalled82AssertNoBearer(t, read)
			opportunities := crmInstalled82Run(t, binary, env, "", "--profile", profile.name, "--json", "crm-opportunities", "list", "--filters", `{"status":"open"}`)
			if opportunities.code != 0 || !strings.Contains(opportunities.stdout, profile.opportunityID) || !strings.Contains(opportunities.stdout, profile.company) {
				t.Fatalf("selected profile read another sandbox's CRM rows: %s", opportunities.stdout)
			}
			for _, otherCompany := range []string{crmInstalled82A, crmInstalled82B} {
				if otherCompany != profile.company && strings.Contains(opportunities.stdout, otherCompany) {
					t.Fatalf("selected profile exposed company %s: %s", otherCompany, opportunities.stdout)
				}
			}
			crmInstalled82AssertNoBearer(t, opportunities)
		}
		for _, entry := range env {
			if strings.HasPrefix(entry, "XDG_CONFIG_HOME=") {
				path := filepath.Join(strings.TrimPrefix(entry, "XDG_CONFIG_HOME="), "factuarea", "config.toml")
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("native fallback config is not private")
				}
			}
		}
		missing := crmInstalled82Run(t, binary, env, "", "--profile", "not-stored", "--json", "whoami")
		if missing.code != 2 {
			t.Fatal("missing profile fell back to another tenant key")
		}
	})
	t.Run("explicit_cursor_preserves_company_transport_and_filters", func(t *testing.T) {
		var reads atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/account" {
				crmInstalled82Account(w, r, []string{"crm_opportunities:read"})
				return
			}
			if r.URL.Path != "/v1/crm/opportunity" || r.URL.Query().Get("filters") != `{"status":"open"}` {
				t.Error("native endpoint/filters changed")
			}
			page := reads.Add(1)
			w.Header().Set("Content-Type", "application/json")
			if page == 1 {
				fmt.Fprintf(w, `{"data":{"items":[{"id":%q,"amount":"9000000000000000.0001"}],"next_cursor":"native+/=opaque"}}`, crmInstalled82A)
			} else {
				if r.URL.Query().Get("cursor") != "native+/=opaque" {
					t.Error("opaque cursor changed")
				}
				fmt.Fprintf(w, `{"data":{"items":[{"id":%q,"amount":"0.00"}],"next_cursor":null}}`, crmInstalled82B)
			}
		}))
		defer server.Close()
		result := crmInstalled82Run(t, binary, crmInstalled82Environment(t, server.URL), "", "--json", "crm-opportunities", "list", "--filters", `{"status":"open"}`, "--all", "--max-pages", "2")
		lines := strings.Split(strings.TrimSpace(result.stdout), "\n")
		if result.code != 0 || reads.Load() != 2 || len(lines) != 2 || !json.Valid([]byte(lines[0])) || !json.Valid([]byte(lines[1])) || !strings.Contains(result.stdout, `"9000000000000000.0001"`) {
			t.Fatal("installed native cursor output differs")
		}
	})
	t.Run("single_page_keeps_opaque_cursor_and_limit_without_prefetch", func(t *testing.T) {
		const filters = ` {"status":"open","search":"Àngela 山 +/="} `
		const cursor = "https://cursor.example.test/private?opaque=+/= 山"
		const body = ` {"data":{"items":[{"id":"01900000-0000-7000-8000-000000000001","amount":9000000000000000.0001,"optional":null}],"next_cursor":"next opaque +/= cursor"}} `
		var accounts, reads atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+crmInstalled82KeyA {
				t.Error("single-page request changed its credential or method")
			}
			if r.URL.Path == "/v1/account" {
				accounts.Add(1)
				crmInstalled82Account(w, r, []string{"crm_opportunities:read"})
				return
			}
			reads.Add(1)
			query := r.URL.Query()
			if r.URL.Path != "/v1/crm/opportunity" || query.Get("filters") != filters || query.Get("cursor") != cursor || query.Get("limit") != "25" || len(query) != 3 {
				t.Error("single-page route, filters, opaque cursor or limit changed")
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, body)
		}))
		defer server.Close()
		result := crmInstalled82Run(t, binary, crmInstalled82Environment(t, server.URL), "", "--json", "crm-opportunities", "list", "--filters", filters, "--cursor", cursor, "--limit", "25")
		if result.code != 0 || result.stderr != "" || result.stdout != body+"\n" || accounts.Load() != 1 || reads.Load() != 1 {
			t.Fatal("installed default read prefetched or reconstructed the native page")
		}
		crmInstalled82AssertNoBearer(t, result)
	})
	t.Run("cursor_budget_returns_partial_and_exact_next_cursor", func(t *testing.T) {
		var reads atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/account" {
				crmInstalled82Account(w, r, []string{"crm_activities:read"})
				return
			}
			reads.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":{"items":[],"next_cursor":"private-next-cursor"}}`)
		}))
		defer server.Close()
		result := crmInstalled82Run(t, binary, crmInstalled82Environment(t, server.URL), "", "--json", "crm-activities", "index", "--all", "--max-pages", "1")
		if result.code != 5 || reads.Load() != 1 || !strings.Contains(result.stderr, `"partial":true`) || !strings.Contains(result.stderr, `"next_cursor":"private-next-cursor"`) {
			t.Fatal("installed budget restarted or lost continuation")
		}
	})
	for _, fixture := range []struct{ status, exit int }{{403, 4}, {404, 6}, {409, 8}, {422, 5}, {429, 7}} {
		t.Run(fmt.Sprintf("HTTP_%d_preserves_native_fields_and_exit%d", fixture.status, fixture.exit), func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmInstalled82Account(w, r, []string{"crm_opportunities:write"})
					return
				}
				writes.Add(1)
				body, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || r.URL.Path != "/v1/crm/opportunity/"+crmInstalled82A+"/stage" || !strings.Contains(string(body), `"expected_version":3`) {
					t.Error("native CAS/owner path changed")
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(fixture.status)
				fmt.Fprintf(w, `{"error":{"type":"owner_specific","message":"rejected %s","details":{"field_errors":{"expected_version":["stale"]},"required_scope":"crm_opportunities:write"}},"errors":{"target_stage_id":["invalid"]}}`, crmInstalled82KeyA)
			}))
			defer server.Close()
			result := crmInstalled82Run(t, binary, crmInstalled82Environment(t, server.URL), "", "--json", "crm-opportunities", "move-stage", crmInstalled82A, "-d", `{"expected_version":3,"expected_definition_version":0,"target_pipeline_id":"01900000-0000-7000-8000-000000000001","target_stage_id":"01900000-0000-7000-8000-000000000002"}`)
			if result.code != fixture.exit || writes.Load() != 1 || result.stdout != "" || !json.Valid([]byte(result.stderr)) || !strings.Contains(result.stderr, "field_errors") || !strings.Contains(result.stderr, "required_scope") || !strings.Contains(result.stderr, `"retry_after":"0"`) {
				t.Fatal("installed typed failure changed or retried")
			}
			crmInstalled82AssertNoBearer(t, result)
		})
	}
	t.Run("CAS_409_redacts_response_headers_and_nested_diagnostics", func(t *testing.T) {
		var writes atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/account" {
				crmInstalled82Account(w, r, []string{"crm_opportunities:write"})
				return
			}
			writes.Add(1)
			if r.Method != http.MethodPost || r.URL.Path != "/v1/crm/opportunity/"+crmInstalled82A+"/stage" || r.Header.Get("Idempotency-Key") != "batch82-cas-first" {
				t.Error("installed conflict changed its CAS path or original attempt")
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Request-Id", crmInstalled82KeyA)
			w.Header().Set("Retry-After", crmInstalled82KeyA)
			w.WriteHeader(http.StatusConflict)
			fmt.Fprintf(w, `{"error":{"type":"owner_conflict","message":"stale %s","details":{"field_errors":{"expected_version":["%s"]}}},"errors":{"target_stage_id":["%s"]},"data":{"diagnostic":"%s"}}`, crmInstalled82KeyA, crmInstalled82KeyA, crmInstalled82KeyA, crmInstalled82KeyA)
		}))
		defer server.Close()
		result := crmInstalled82Run(t, binary, crmInstalled82Environment(t, server.URL), "", "--json", "crm-opportunities", "move-stage", crmInstalled82A, "--idempotency-key", "batch82-cas-first", "-d", `{"expected_version":3,"expected_definition_version":0,"target_pipeline_id":"01900000-0000-7000-8000-000000000001","target_stage_id":"01900000-0000-7000-8000-000000000002"}`)
		if result.code != 8 || writes.Load() != 1 || result.stdout != "" || !json.Valid([]byte(result.stderr)) {
			t.Fatal("installed definitive conflict became an unconfirmed write or was retried")
		}
		for _, field := range []string{"expected_version", "target_stage_id", `"http_status":409`, `"retry_after":"[REDACTED]"`, `"request_id":"[REDACTED]"`, `"diagnostic":"[REDACTED]"`} {
			if !strings.Contains(result.stderr, field) {
				t.Fatalf("installed conflict lost redacted native field %q", field)
			}
		}
		crmInstalled82AssertNoBearer(t, result)
	})
	t.Run("read_scope_denial_has_exit4_without_write", func(t *testing.T) {
		var writes atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/account" {
				writes.Add(1)
			}
			crmInstalled82Account(w, r, []string{"crm_leads:read"})
		}))
		defer server.Close()
		result := crmInstalled82Run(t, binary, crmInstalled82Environment(t, server.URL), "", "--json", "crm-leads", "create", "-d", `{"name":"Scope fixture"}`)
		if result.code != 4 || writes.Load() != 0 {
			t.Fatal("read scope implied write authority")
		}
	})
	t.Run("lost_write_response_exit11_recovers_same_original_only_with_GET", func(t *testing.T) {
		wire := " \n{\"name\":\"Synthetic Àngela 山\",\"notes\":null}\n"
		sum := sha256.Sum256([]byte(wire))
		digest := hex.EncodeToString(sum[:])
		var writes, reads atomic.Int32
		original := ` { "data": { "id": "01900000-0000-7000-8000-000000000001", "confirmed":true, "representation_available":false } } `
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/account" {
				crmInstalled82Account(w, r, []string{"crm_leads:write"})
				return
			}
			if r.Method == "POST" {
				writes.Add(1)
				body, _ := io.ReadAll(r.Body)
				if string(body) != wire || r.Header.Get("Idempotency-Key") != "batch82 original key" {
					t.Error("actual first wire/key changed")
				}
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error("fixture could not drop response")
					return
				}
				connection.Close()
				return
			}
			reads.Add(1)
			if r.Method != "GET" || r.URL.Path != "/v1/crm/leads/create/original" || r.URL.Query().Get("original_body_sha256") != digest || r.Header.Get("Idempotency-Key") != "batch82 original key" {
				t.Error("GET recovery identity changed")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"state": "confirmed", "original_body_sha256": digest, "original_response": map[string]any{"status": 201, "body": original}}, "meta": map[string]any{"authorization_version": "synthetic-current-reader"}})
		}))
		defer server.Close()
		env := crmInstalled82Environment(t, server.URL)
		first := filepath.Join(t.TempDir(), "private-first.json")
		result := crmInstalled82Run(t, binary, env, "", "--json", "crm-leads", "create", "--idempotency-key", "batch82 original key", "--first-body-output", first, "-d", wire)
		if result.code != 11 || writes.Load() != 1 || reads.Load() != 0 || !strings.Contains(result.stderr, digest) || strings.Contains(result.stderr, "Synthetic Àngela") {
			t.Fatal("installed ambiguous create retried or disclosed private body")
		}
		crmInstalled82AssertNoBearer(t, result)
		actual, err := os.ReadFile(first)
		if err != nil || string(actual) != wire {
			t.Fatal("actual installed first bytes were not retained")
		}
		info, _ := os.Stat(first)
		if info.Mode().Perm() != 0600 {
			t.Fatal("attempt file is not private")
		}
		recovered := crmInstalled82Run(t, binary, env, "", "--json", "crm", "originals", "lead-create", "--first-body-file", first, "--idempotency-key", "batch82 original key")
		var payload struct {
			Data struct {
				Original struct {
					Body string `json:"body"`
				} `json:"original_response"`
			} `json:"data"`
		}
		if recovered.code != 0 || writes.Load() != 1 || reads.Load() != 1 || json.Unmarshal([]byte(recovered.stdout), &payload) != nil || payload.Data.Original.Body != original {
			t.Fatal("installed original GET reconstructed or repeated create")
		}
	})
	t.Run("terminal_table_is_emitted_by_real_binary_without_control_sequences", func(t *testing.T) {
		script, err := exec.LookPath("script")
		if err != nil {
			t.Skip("native PTY verification requires the system script utility")
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/account" {
				crmInstalled82Account(w, r, []string{"crm_leads:read"})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"data":{"id":%q,"version":3,"status":"open","name":"Synthetic\u001b[31m row"}}`, crmInstalled82A)
		}))
		defer server.Close()
		env := crmInstalled82Environment(t, server.URL)
		quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
		process := exec.Command(script, "-q", "-e", "-c", quote(binary)+" crm-leads show "+quote(crmInstalled82A), "/dev/null")
		process.Env = env
		wire, err := process.CombinedOutput()
		if err != nil || !bytes.Contains(wire, []byte("VERSIÓN")) || !bytes.Contains(wire, []byte(crmInstalled82A)) || bytes.Contains(wire, []byte("\x1b[31m")) {
			t.Fatal("real terminal did not receive a readable sanitized table")
		}
	})
}
