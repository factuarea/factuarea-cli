package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

const crmTestID = "019c0511-1111-7111-8111-111111111111"
const crmTestOtherID = "019c0511-1111-7111-8111-222222222222"
const crmTestKey = "cli-original-intention-34"

func crmRun(t *testing.T, server *httptest.Server, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", server.URL)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cmd := NewRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}
func crmAccount(w http.ResponseWriter, scopes ...string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"api_key": map[string]any{"scopes": scopes}}})
}

func TestCRMCommandsDiscoverOnlyNativeOperations(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"commands", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var entries []manifestEntry
	if err := json.Unmarshal(out.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	count := 0
	nativeIDs := map[string]bool{}
	for _, entry := range entries {
		if entry.CrmOperation == "" {
			continue
		}
		count++
		if nativeIDs[entry.OperationID] {
			t.Fatalf("duplicate original operation %s", entry.OperationID)
		}
		nativeIDs[entry.OperationID] = true
		if !strings.HasPrefix(entry.Command, "factuarea crm ") || entry.RequiredScope == "" {
			t.Fatalf("native command metadata lost: %+v", entry)
		}
		if strings.HasPrefix(entry.OperationID, "public-api.v1.") {
			t.Fatalf("original native operation was renamed: %s", entry.OperationID)
		}
	}
	if count != 49 {
		t.Fatalf("native command count=%d expected49", count)
	}
	for _, unsupported := range []string{"opportunities", "activities"} {
		cmd := NewRootCmd()
		cmd.SetArgs([]string{"crm", unsupported, "list"})
		if err := cmd.Execute(); err == nil {
			t.Fatalf("unsupported group%s was invented", unsupported)
		}
	}
}

func TestCRMHelpUsesNativeCursorAndHasNoUUIDClientMint(t *testing.T) {
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"crm", "leads", "list", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"--cursor", "--filters", "--limit", "--paginate", "--max-pages"} {
		if !strings.Contains(out.String(), expected) {
			t.Errorf("native help missing%s", expected)
		}
	}
	if strings.Contains(out.String(), "starting_after") {
		t.Fatal("legacy cursor invented for CRM")
	}
}

func TestCRMPathUUIDv7RejectsForeignShapesBeforeNetwork(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer s.Close()
	for _, bad := range []string{"123", "983aa08e-65a1-4c5e-a9ee-9c43aec7a100", "019c0511-1111-7111-0111-111111111111", "%2fother"} {
		t.Run(bad, func(t *testing.T) {
			_, _, err := crmRun(t, s, "crm", "leads", "show", bad, "--json")
			if err == nil || exit.ForError(err) != exit.Usage {
				t.Fatalf("bad publicid%s accepted: %v", bad, err)
			}
		})
	}
	if calls != 0 {
		t.Fatalf("invalid path identity contacted server%d times", calls)
	}
}

func TestCRMNativeLeadListKeepsOpaqueCursorAndMasks(t *testing.T) {
	pages := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "crm_leads:read")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/crm/leads" || r.URL.Query().Get("filters") != `{"score_min":0}` {
			t.Errorf("original path/filter changed%s", r.URL.String())
		}
		pages++
		if pages == 1 {
			io.WriteString(w, `{"data":{"data":[{"id":"`+crmTestID+`","version":4,"name":"Visible"}],"has_more":true,"next_cursor":"signed+/cursor=="}}`)
		} else {
			if r.URL.Query().Get("cursor") != "signed+/cursor==" {
				t.Error("opaque cursor changed")
			}
			io.WriteString(w, `{"data":{"data":[{"id":"`+crmTestOtherID+`","version":7}],"has_more":false,"next_cursor":null}}`)
		}
	}))
	defer s.Close()
	out, stderr, err := crmRun(t, s, "crm", "leads", "list", "--filters", `{"score_min":0}`, "--paginate", "--max-pages", "2", "--json")
	if err != nil || pages != 2 {
		t.Fatalf("pages%d err%v stderr%s", pages, err, stderr)
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	if len(rows) != 2 || rows[1]["name"] != nil || strings.Contains(out, "fact_test_") {
		t.Fatalf("hidden field invented or credential disclosed:%s", out)
	}
}

func TestCRMReadCredentialCannotDispatchWrite(t *testing.T) {
	writes := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "crm_leads:read")
			return
		}
		writes++
	}))
	defer s.Close()
	_, stderr, err := crmRun(t, s, "crm", "leads", "create", "-d", `{"name":"Draft"}`, "--json", "--idempotency-key", crmTestKey)
	if exit.ForError(err) != exit.Perm || writes != 0 || !strings.Contains(stderr, "crm_leads:write") {
		t.Fatalf("writes%d err%v stderr%s", writes, err, stderr)
	}
}

func TestCRMOriginalBodyAndCASPreserveExactNumbers(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "crm_leads:write")
			return
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		if r.Method != "PATCH" || r.URL.Path != "/v1/crm/leads/"+crmTestID || r.Header.Get("Idempotency-Key") != crmTestKey || !strings.Contains(string(body), `"expected_version":9007199254740993`) || !strings.Contains(string(body), `"email":null`) || strings.Contains(string(body), "phone") {
			t.Errorf("original request changed:%s %s %s", r.Method, r.URL, body)
		}
		w.WriteHeader(409)
		io.WriteString(w, `{"error":{"type":"conflict_error","code":"crm_version_conflict","message":"Versión cambiada","details":{"expected_version":9007199254740993},"errors":{"expected_version":["Revisa la versión original"]}}}`)
	}))
	defer s.Close()
	_, stderr, err := crmRun(t, s, "crm", "leads", "update", crmTestID, "-d", `{"expected_version":9007199254740993,"email":null}`, "--json", "--idempotency-key", crmTestKey)
	if calls != 1 || exit.ForError(err) != exit.Conflict || !strings.Contains(stderr, `"expected_version":9007199254740993`) || !strings.Contains(stderr, "Revisa la versión original") {
		t.Fatalf("calls%d err%v stderr%s", calls, err, stderr)
	}
}

func TestCRMNativeValidationPermissionAndInvisibilityErrorsRemainOriginal(t *testing.T) {
	for _, status := range []int{403, 404, 422, 429} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "crm_leads:write")
					return
				}
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				io.WriteString(w, `{"error":{"code":"native_original_error","message":"Denegación original","details":{"field_errors":{"name":["No autorizado"]}}},"data":{"state":"unavailable"}}`)
			}))
			defer s.Close()
			out, stderr, err := crmRun(t, s, "crm", "leads", "create", "-d", `{"name":"Draft"}`, "--json", "--idempotency-key", crmTestKey)
			want := map[int]int{403: exit.Perm, 404: exit.NotFound, 422: exit.Validation, 429: exit.RateLimit}[status]
			if calls != 1 || exit.ForError(err) != want || out != "" || !strings.Contains(stderr, "native_original_error") || !strings.Contains(stderr, "No autorizado") || strings.Contains(stderr, "fact_test_") {
				t.Fatalf("status%d calls%d err%v stdout%s stderr%s", status, calls, err, out, stderr)
			}
		})
	}
}

func TestCRMServerFailureDoesNotAutomaticallyRedispatchOriginalWrite(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "crm_leads:write")
			return
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		io.WriteString(w, `{"error":{"type":"service_unavailable_error","code":"crm_operation_unavailable","message":"Productor no disponible"}}`)
	}))
	defer s.Close()
	_, stderr, err := crmRun(t, s, "crm", "leads", "create", "-d", `{"name":"Draft"}`, "--json", "--idempotency-key", crmTestKey)
	if calls != 1 || exit.ForError(err) != exit.Server || !strings.Contains(stderr, "crm_operation_unavailable") {
		t.Fatalf("calls%d err%v stderr%s", calls, err, stderr)
	}
}

func TestCRMTransportLossRetainsOriginalIntentWithoutRedispatch(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "crm_leads:write")
			return
		}
		calls++
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		connection.Close()
	}))
	defer s.Close()
	out, stderr, err := crmRun(t, s, "crm", "leads", "create", "-d", `{"name":"Draft"}`, "--json", "--idempotency-key", crmTestKey)
	if calls != 1 || exit.ForError(err) != exit.Network || out != "" || !strings.Contains(stderr, "unconfirmed") || !strings.Contains(stderr, crmTestKey) || strings.Contains(stderr, "fact_test_") {
		t.Fatalf("calls%d err%v out%s stderr%s", calls, err, out, stderr)
	}
}

func TestCRMNativeDestructiveConfirmationIsRequired(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":{"confirmed":true}}`)
	}))
	defer s.Close()
	_, _, err := crmRun(t, s, "crm", "leads", "delete", crmTestID, "-d", `{"expected_version":4,"confirmed":true}`, "--skip-scope-check", "--no-input", "--json")
	if err == nil || exit.ForError(err) != exit.Usage || calls != 0 {
		t.Fatalf("unconfirmed effect called server%d times err%v", calls, err)
	}
}

func TestCRMNativeDryRunDoesNotCallAPIOrGenerateEntityUUID(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer s.Close()
	out, _, err := crmRun(t, s, "crm", "leads", "create", "-d", `{"name":"Human draft","email":null}`, "--dry-run", "--json")
	if err != nil || calls != 0 || strings.Contains(out, `"id"`) || !strings.Contains(out, `"email":null`) {
		t.Fatalf("calls%d err%v draft%s", calls, err, out)
	}
}

func TestCRMCommandContextCancellationDoesNotDispatch(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer s.Close()
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", s.URL)
	root := NewRootCmd()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root.SetContext(ctx)
	root.SetArgs([]string{"crm", "leads", "show", crmTestID, "--skip-scope-check", "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	if calls != 0 || exit.ForError(err) != exit.Network {
		t.Fatalf("calls%d err%v", calls, err)
	}
}

func TestCRMRedirectDoesNotRedispatchOriginalWrite(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "crm_leads:write")
			return
		}
		calls++
		w.Header().Set("Location", "/v1/crm/leads/redirected")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer s.Close()
	out, _, err := crmRun(t, s, "crm", "leads", "create", "-d", `{"name":"Draft"}`, "--json", "--idempotency-key", crmTestKey)
	if calls != 1 || err == nil || out != "" {
		t.Fatalf("calls%d err%v stdout%s", calls, err, out)
	}
}

func TestCRMIncompleteSuccessCannotFabricateOriginalReceipt(t *testing.T) {
	for _, body := range []string{"", `{"data":null}`, `{"ok":true}`, `{"data":{"id":"unconfirmed"}`} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, "crm_leads:write")
					return
				}
				calls++
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, body)
			}))
			defer s.Close()
			out, stderr, err := crmRun(t, s, "crm", "leads", "create", "-d", `{"name":"Draft"}`, "--json", "--idempotency-key", crmTestKey)
			if calls != 1 || exit.ForError(err) != exit.Network || out != "" || !strings.Contains(stderr, crmTestKey) || !strings.Contains(stderr, "cli_mutation_unconfirmed") {
				t.Fatalf("calls%d err%v stdout%s stderr%s", calls, err, out, stderr)
			}
		})
	}
}

func TestCRMTruncatedAcknowledgementRetainsOriginalIntent(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			crmAccount(w, "crm_leads:write")
			return
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "200")
		io.WriteString(w, `{"data":{"id":"`+crmTestID+`"}}`)
	}))
	defer s.Close()
	out, stderr, err := crmRun(t, s, "crm", "leads", "create", "-d", `{"name":"Draft"}`, "--json", "--idempotency-key", crmTestKey)
	if calls != 1 || exit.ForError(err) != exit.Network || out != "" || !strings.Contains(stderr, crmTestKey) {
		t.Fatalf("calls%d err%v stdout%s stderr%s", calls, err, out, stderr)
	}
}
