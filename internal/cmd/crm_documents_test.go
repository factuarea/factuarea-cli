package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/exit"
	"github.com/factuarea/factuarea-cli/internal/output"
)

const crmDocumentTestID = "01900000-0000-7000-8000-000000000001"
const crmDocumentTestOtherID = "01900000-0000-7000-8000-000000000002"
const crmDocumentTestNativeID = "01900000-0000-4000-8000-000000000001"

func executeCRMDocumentTest(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"--json", "crm"}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func setupCRMDocumentTest(t *testing.T, url string) {
	t.Helper()
	t.Setenv("FACTUAREA_API_KEY", "fact_test_"+strings.Repeat("a", 24))
	t.Setenv("FACTUAREA_BASE_URL", url)
	t.Setenv("FACTUAREA_PROFILE", "documents-native")
}

func crmDocumentTestNative(kind string) string {
	amounts, applicable := `"quoted_amount":"999999999999999999.99","invoiced_amount":"0.00","paid_amount":"0.00"`, "applicable"
	if kind == "contract" || kind == "recurring_schedule" {
		amounts, applicable = `"quoted_amount":null,"invoiced_amount":null,"paid_amount":null`, "not_applicable"
	}
	return fmt.Sprintf(`{"id":%q,"type":%q,"version":9,"state":"draft","number":null,"currency":"EUR",%s,"financial_applicability":%q,"valid_from":null,"valid_until":null}`, crmDocumentTestNativeID, kind, amounts, applicable)
}

func crmDocumentTestLink(kind string, primary bool) string {
	return fmt.Sprintf(`{"id":%q,"version":2,"opportunity_id":%q,"document_type":%q,"document_id":%q,"is_primary":%t,"status":"linked","document":%s}`, crmDocumentTestOtherID, crmDocumentTestID, kind, crmDocumentTestNativeID, primary, crmDocumentTestNative(kind))
}

func crmDocumentTestResponse(op crmDocumentOperation) string {
	switch {
	case op.group == "documents" && op.action == "list":
		return fmt.Sprintf(`{"data":{"items":[{"id":%q,"version":1}],"total":1,"page":2,"per_page":3}}`, crmDocumentTestID)
	case op.group == "documents" && op.action == "get":
		return fmt.Sprintf(`{"data":{"id":%q,"version":1}}`, crmDocumentTestID)
	case op.group == "documents":
		version := 2
		if op.action == "upload" {
			version = 1
		}
		return fmt.Sprintf(`{"data":{"id":%q,"version":%d}}`, crmDocumentTestID, version)
	case op.action == "list":
		return fmt.Sprintf(`{"data":{"opportunity_id":%q,"items":[%s],"next_cursor":null,"total":1,"pending_count":0}}`, crmDocumentTestID, crmDocumentTestLink("quote", false))
	case op.action == "candidates":
		return fmt.Sprintf(`{"data":{"opportunity_id":%q,"document_type":"quote","items":[{"id":%q,"type":"quote","revision":9,"state":"draft","label":null,"currency":"EUR","valid_from":null,"valid_until":null,"financial_applicability":"applicable"}],"next_cursor":null,"available":true,"unavailable_reason":null}}`, crmDocumentTestID, crmDocumentTestNativeID)
	case op.action == "financial-summary":
		return fmt.Sprintf(`{"data":{"opportunity_id":%q,"version":8,"currencies":[{"currency":"EUR","forecast":"999999999999999999.9999","quoted":"999999999999999999.99","invoiced":"0.00","paid":"0.00"},{"currency":"USD","forecast":"1.0000","quoted":"1.00","invoiced":"0.00","paid":"0.00"}]}}`, crmDocumentTestID)
	case op.action == "unlink":
		return fmt.Sprintf(`{"data":{"id":%q,"version":2,"opportunity_id":%q,"document_type":"quote","document_id":%q}}`, crmDocumentTestOtherID, crmDocumentTestID, crmDocumentTestNativeID)
	default:
		return `{"data":` + crmDocumentTestLink("quote", true) + `}`
	}
}

func TestCRMDocumentsFourteenNativeRoutesAndPrivateIntent(t *testing.T) {
	for _, op := range crmDocumentOperations {
		t.Run(op.group+"/"+op.action, func(t *testing.T) {
			calls, preflight := 0, 0
			args := []string{}
			for _, name := range op.args {
				id := crmDocumentTestID
				if name == "link" {
					id = crmDocumentTestOtherID
				}
				args = append(args, id)
			}
			body := " \n" + crmDocumentExamples[op.action] + "\n "
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fact_test_"+strings.Repeat("a", 24) || r.Header.Get("X-Active-Profile") != crmDocumentTestOtherID || r.Header.Get("Factuarea-Version") != "2026-10-09" {
					t.Error("native credential/profile/version headers changed")
				}
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v1/account" {
					preflight++
					fmt.Fprintf(w, `{"data":{"api_key":{"scopes":[%q]}}}`, op.scope())
					return
				}
				calls++
				if r.Method != op.method || r.URL.Path != op.pathFor(args) {
					t.Errorf("wrong native route: %s %s", r.Method, r.URL.Path)
				}
				if op.mutating() {
					wire, _ := io.ReadAll(r.Body)
					if string(wire) != body || r.Header.Get("Idempotency-Key") != "Original-Key !" || r.Header.Get("Content-Type") != "application/json" {
						t.Error("original JSON bytes/key changed or multipart used")
					}
				}
				if op.pagination == "page" && (r.URL.Query().Get("page") != "2" || r.URL.Query().Get("per_page") != "3" || r.URL.Query().Get("archived") != "false") {
					t.Error("library page/explicit false filter lost")
				}
				if op.group == "opportunity-documents" && op.action == "list" && (r.URL.Query().Get("filters") != `{"is_primary":false,"status":"linked"}` || r.URL.Query().Get("cursor") != "opaque+token/==") {
					t.Error("typed filters or opaque cursor changed")
				}
				if op.action == "download" {
					if r.URL.Query().Get("document_version") != "7" {
						t.Error("download version changed")
					}
					w.Header().Set("Content-Type", "application/pdf")
					w.Header().Set("Content-Disposition", `attachment; filename="native.pdf"`)
					fmt.Fprint(w, "%PDF-native\x00bytes")
					return
				}
				w.WriteHeader(op.status())
				fmt.Fprint(w, crmDocumentTestResponse(op))
			}))
			defer server.Close()
			setupCRMDocumentTest(t, server.URL)
			cli := append([]string{op.group, op.action}, args...)
			cli = append(cli, "--api-version", "2026-10-09", "--active-profile", crmDocumentTestOtherID)
			intentFile := filepath.Join(t.TempDir(), "intent.json")
			if op.mutating() {
				cli = append(cli, "--data", body, "--idempotency-key", "Original-Key !", "--intent-file", intentFile)
				if op.confirmation() {
					cli = append(cli, "--confirm", args[len(args)-1])
				}
			}
			if op.pagination == "page" {
				cli = append(cli, "--page", "2", "--per-page", "3", "--archived=false")
			}
			if op.group == "opportunity-documents" && op.action == "list" {
				cli = append(cli, "--status", "linked", "--is-primary=false", "--cursor", "opaque+token/==")
			}
			if op.action == "candidates" {
				cli = append(cli, "--document-type", "quote")
			}
			outputFile := filepath.Join(t.TempDir(), "native.pdf")
			if op.action == "download" {
				cli = append(cli, "--document-version", "7", "--output", outputFile)
			}
			stdout, stderr, err := executeCRMDocumentTest(t, cli...)
			if err != nil || calls != 1 || preflight != 1 || stderr != "" || !json.Valid([]byte(stdout)) {
				t.Fatalf("native operation failed: calls=%d preflight=%d err=%v stdout=%s stderr=%s", calls, preflight, err, stdout, stderr)
			}
			if op.mutating() {
				intent, err := loadCRMDocumentIntent(intentFile)
				if err != nil || intent.Body != body || intent.Key != "Original-Key !" || strings.Contains(intent.Body, "fact_test_") {
					t.Fatal("checkpoint did not preserve original intent")
				}
				wire, _ := os.ReadFile(intentFile)
				if strings.Contains(string(wire), "fact_test_") {
					t.Fatal("checkpoint copied secret")
				}
				st, _ := os.Stat(intentFile)
				if st.Mode().Perm() != 0600 {
					t.Fatal("checkpoint is not private")
				}
			}
			if op.action == "download" {
				got, _ := os.ReadFile(outputFile)
				st, _ := os.Stat(outputFile)
				if string(got) != "%PDF-native\x00bytes" || st.Mode().Perm() != 0600 {
					t.Fatal("private bytes changed/exposed")
				}
			}
			if op.action == "financial-summary" && !strings.Contains(stdout, `"forecast":"999999999999999999.9999"`) {
				t.Fatal("decimal string precision lost")
			}
			if op.group == "documents" && op.action == "get" && strings.Contains(stdout, `"title"`) {
				t.Fatal("masked field fabricated")
			}
		})
	}
}

func TestCRMDocumentsRecoveryKeepsOriginalBytesCASAndOneEffect(t *testing.T) {
	calls, effects := 0, 0
	key := "Original-Recovery KEY/42"
	body := "\n {\"expected_version\":7, \"grant_type\":\"user\",\"grantee_id\":\"" + strings.ToUpper(crmDocumentTestNativeID) + "\",\"expires_at\":null}\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/v1/crm/documents/"+crmDocumentTestID+"/shares" || r.Header.Get("Idempotency-Key") != key || r.Header.Get("X-Active-Profile") != crmDocumentTestOtherID || r.Header.Get("Factuarea-Version") != "2026-10-09" {
			t.Error("recovery changed original transport intent")
		}
		wire, _ := io.ReadAll(r.Body)
		if string(wire) != body {
			t.Error("recovery changed original body/CAS/null/native UUID spelling")
		}
		if calls == 1 {
			effects++
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			conn.Close()
			return
		}
		// The owner's latest head could be 99. Replay selects original v8.
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"id":%q,"version":8}}`, crmDocumentTestID)
	}))
	defer server.Close()
	setupCRMDocumentTest(t, server.URL)
	intentFile := filepath.Join(t.TempDir(), "intent.json")
	stdout, stderr, err := executeCRMDocumentTest(t, "documents", "share", crmDocumentTestID, "--data", body, "--idempotency-key", key, "--intent-file", intentFile, "--active-profile", crmDocumentTestOtherID, "--api-version", "2026-10-09", "--skip-scope-check")
	if err == nil || stdout != "" || calls != 1 || !strings.Contains(stderr, `"unconfirmed":true`) || exit.ForError(err) != exit.Network {
		t.Fatalf("ambiguous effect retried/confirmed: %v %s", err, stderr)
	}
	if !strings.Contains(stderr, "--profile 'documents-native'") {
		t.Fatal("recovery hint lost the original native profile")
	}
	original, _ := os.ReadFile(intentFile)
	before := sha256.Sum256(original)
	stdout, stderr, err = executeCRMDocumentTest(t, "documents", "recover", "--intent-file", intentFile, "--skip-scope-check")
	if err != nil || stderr != "" || !strings.Contains(stdout, `"version":8`) || calls != 2 || effects != 1 {
		t.Fatalf("original replay failed/duplicated effect: %v %s", err, stderr)
	}
	after, _ := os.ReadFile(intentFile)
	if sha256.Sum256(after) != before {
		t.Fatal("recovery rewrote original checkpoint")
	}
}

func TestCRMDocumentsRecoveryRejectsChangedBindingTamperingAndOwner(t *testing.T) {
	for _, mode := range []string{"credential", "environment", "profile", "server", "body", "key", "path", "owner", "public-mode", "symlink", "override"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				fmt.Fprintf(w, `{"data":{"id":%q,"version":1}}`, crmDocumentTestID)
			}))
			defer server.Close()
			setupCRMDocumentTest(t, server.URL)
			intentFile := filepath.Join(t.TempDir(), "intent.json")
			_, _, err := executeCRMDocumentTest(t, "documents", "upload", "--data", crmDocumentExamples["upload"], "--idempotency-key", "bound-original", "--intent-file", intentFile, "--skip-scope-check")
			if err != nil {
				t.Fatal(err)
			}
			calls = 0
			group := "documents"
			extra := []string{}
			switch mode {
			case "credential":
				t.Setenv("FACTUAREA_API_KEY", "fact_test_"+strings.Repeat("b", 24))
			case "environment":
				t.Setenv("FACTUAREA_API_KEY", "fact_live_"+strings.Repeat("a", 24))
				extra = append(extra, "--live")
			case "profile":
				extra = append(extra, "--profile", "different")
			case "server":
				t.Setenv("FACTUAREA_BASE_URL", server.URL+"/different")
			case "owner":
				group = "opportunity-documents"
			case "public-mode":
				if err := os.Chmod(intentFile, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(intentFile, alias); err != nil {
					t.Fatal(err)
				}
				intentFile = alias
			case "override":
				extra = append(extra, "--idempotency-key", "new-key")
			default:
				intent, err := loadCRMDocumentIntent(intentFile)
				if err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "body":
					intent.Body = strings.Replace(intent.Body, "Oferta", "Altered", 1)
				case "key":
					intent.Key = "new-key"
				case "path":
					intent.Path += "/archive"
				}
				wire, _ := json.Marshal(intent)
				if err := os.WriteFile(intentFile, wire, 0600); err != nil {
					t.Fatal(err)
				}
			}
			args := append([]string{group, "recover", "--intent-file", intentFile, "--skip-scope-check"}, extra...)
			stdout, _, err := executeCRMDocumentTest(t, args...)
			if err == nil || calls != 0 || stdout != "" {
				t.Fatalf("changed intent/context dispatched: mode=%s calls=%d err=%v", mode, calls, err)
			}
		})
	}
}

func TestCRMDocumentsAmbiguousStatusesAndReceiptsNeverRetry(t *testing.T) {
	for _, mode := range []string{"503", "redirect", "403", "404", "queued", "wrong-cas", "wrong-id", "202", "duplicate-json", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Request-Id", "native-req")
				switch mode {
				case "503", "403", "404":
					status := map[string]int{"503": 503, "403": 403, "404": 404}[mode]
					w.WriteHeader(status)
					fmt.Fprint(w, `{"error":{"message":"Owner revoked","code":"owner_unavailable","request_id":"native-req"}}`)
				case "redirect":
					w.Header().Set("Location", "/must-not-dispatch")
					w.WriteHeader(307)
				case "queued":
					fmt.Fprint(w, `{"data":{"status":"queued"}}`)
				case "wrong-cas":
					fmt.Fprintf(w, `{"data":{"id":%q,"version":99}}`, crmDocumentTestID)
				case "wrong-id":
					fmt.Fprintf(w, `{"data":{"id":%q,"version":2}}`, crmDocumentTestOtherID)
				case "202":
					w.WriteHeader(202)
					fmt.Fprintf(w, `{"data":{"id":%q,"version":2}}`, crmDocumentTestID)
				case "duplicate-json":
					fmt.Fprintf(w, `{"data":{"id":%q,"version":99,"version":2}}`, crmDocumentTestID)
				case "truncated":
					w.Header().Set("Content-Length", "1000")
					fmt.Fprintf(w, `{"data":{"id":%q,"version":2}}`, crmDocumentTestID)
				}
			}))
			defer server.Close()
			setupCRMDocumentTest(t, server.URL)
			file := filepath.Join(t.TempDir(), "intent.json")
			stdout, stderr, err := executeCRMDocumentTest(t, "documents", "replace-version", crmDocumentTestID, "--data", crmDocumentExamples["replace-version"], "--idempotency-key", "original", "--intent-file", file, "--skip-scope-check")
			if err == nil || calls != 1 || stdout != "" || !json.Valid([]byte(stderr)) || !strings.Contains(stderr, `"unconfirmed":true`) {
				t.Fatalf("ambiguous result retried/confirmed: %d %v %s", calls, err, stderr)
			}
			if _, err := os.Stat(file); err != nil {
				t.Fatal("ambiguous intent discarded")
			}
		})
	}
}

func TestCRMDocumentsTypedErrorsKeepCodeMessageAndRequestID(t *testing.T) {
	for _, status := range []int{409, 422, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":{"code":"native_error","message":"Original CAS or quota","request_id":"owner-request","errors":{"expected_version":["stale"]}}}`)
			}))
			defer server.Close()
			setupCRMDocumentTest(t, server.URL)
			stdout, stderr, err := executeCRMDocumentTest(t, "documents", "upload", "--data", crmDocumentExamples["upload"], "--idempotency-key", "original", "--intent-file", filepath.Join(t.TempDir(), "intent.json"), "--skip-scope-check")
			var api *apierr.APIError
			if !errors.As(err, &api) || api.StatusCode != status || calls != 1 || stdout != "" || !strings.Contains(stderr, `"request_id":"owner-request"`) || !strings.Contains(stderr, `"message":"Original CAS or quota"`) || strings.Contains(stderr, `"unconfirmed"`) {
				t.Fatalf("native error lost/coerced/retried: %v %s", err, stderr)
			}
			want := map[int]int{409: exit.Conflict, 422: exit.Validation, 429: exit.RateLimit}[status]
			if exit.ForError(err) != want {
				t.Fatal("native exit code changed")
			}
		})
	}
}

func TestCRMDocumentsClosedBodiesRejectAuthorityCoercionAndInventedCAS(t *testing.T) {
	cases := []struct{ action, body string }{
		{"upload", `{"title":"a","producer_reference_id":"` + crmDocumentTestID + `","claimed_mime":"application/pdf","company_id":4}`},
		{"upload", `{"title":"a","title":"b","producer_reference_id":"` + crmDocumentTestID + `","claimed_mime":"application/pdf"}`},
		{"archive", `{"expected_version":"1"}`},
		{"archive", `{"expected_version":1.0}`},
		{"archive", `{"expected_version":null}`},
		{"archive", `{"expected_version":9223372036854775807}`},
		{"link", `{"document_type":"quote","document_id":"` + crmDocumentTestNativeID + `","is_primary":false,"expected_version":9}`},
		{"link", `{"document_type":"recurring_invoice_schedule","document_id":"` + crmDocumentTestNativeID + `","is_primary":false}`},
		{"share", `{"expected_version":1,"grant_type":"public","grantee_id":"` + crmDocumentTestID + `"}`},
		{"share", `{"expected_version":1,"grant_type":"team","grantee_id":"` + crmDocumentTestNativeID + `"}`},
		{"share", `{"expected_version":1,"grant_type":"user","grantee_id":"` + crmDocumentTestNativeID + `","expires_at":"2026-10-09T12:00:00"}`},
		{"create-quote", `{"draft":{"date":"2026-10-09","recipient":{"kind":"unassigned","id":"` + crmDocumentTestID + `"}}}`},
		{"create-quote", `{"draft":{"date":"2026-02-30","recipient":{"kind":"unassigned"}}}`},
		{"create-quote", `{"draft":{"date":"2026-10-09","expiry_date":"2026-10-08","recipient":{"kind":"unassigned"}}}`},
		{"create-quote", `{"draft":{"date":"2026-10-09","recipient":{"kind":"commercial_counterparty","id":"` + crmDocumentTestID + `"},"client_id":"` + crmDocumentTestNativeID + `"}}`},
		{"create-quote", `{"draft":{"date":"2026-10-09","recipient":{"kind":"unassigned"},"total":42}}`},
		{"create-quote", `{"draft":{"date":"2026-10-09","recipient":{"kind":"unassigned","kind":"business_contact","id":"` + crmDocumentTestID + `"}}}`},
		{"create-quote", `{"draft":{"date":"2026-10-09","recipient":{"kind":"unassigned"},"expected_line_version":null}}`},
	}
	for index, c := range cases {
		t.Run(fmt.Sprintf("%d/%s", index, c.action), func(t *testing.T) {
			var op crmDocumentOperation
			for _, candidate := range crmDocumentOperations {
				if candidate.action == c.action {
					op = candidate
					break
				}
			}
			if _, err := validateCRMDocumentBody(op, []byte(c.body)); err == nil {
				t.Fatal("invalid native closed body accepted")
			}
		})
	}
	for _, key := range []string{"", "key\n", "é", " original", "original ", strings.Repeat("x", 256)} {
		if crmDocumentKey(key) == nil {
			t.Error("invalid/rewritten wire key accepted")
		}
	}
	for _, key := range []string{"Original Key/42", strings.Repeat("x", 255)} {
		if err := crmDocumentKey(key); err != nil {
			t.Error(err)
		}
	}
}

func TestCRMDocumentsOfflinePreviewConfirmationAndCheckpointCollision(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"id":%q,"version":2}}`, crmDocumentTestID)
	}))
	defer server.Close()
	setupCRMDocumentTest(t, server.URL)
	t.Setenv("FACTUAREA_API_KEY", "")
	stdout, _, err := executeCRMDocumentTest(t, "documents", "archive", crmDocumentTestID, "--skeleton")
	if err != nil || !json.Valid([]byte(stdout)) || calls != 0 {
		t.Fatal("skeleton required auth/dispatched")
	}
	stdout, _, err = executeCRMDocumentTest(t, "opportunity-documents", "create-quote", crmDocumentTestID, "--data", crmDocumentExamples["create-quote"], "--dry-run")
	if err != nil || !json.Valid([]byte(stdout)) || calls != 0 {
		t.Fatal("dry-run required auth/dispatched")
	}
	setupCRMDocumentTest(t, server.URL)
	file := filepath.Join(t.TempDir(), "intent.json")
	_, _, err = executeCRMDocumentTest(t, "documents", "archive", crmDocumentTestID, "--data", crmDocumentExamples["archive"], "--idempotency-key", "original", "--intent-file", file, "--skip-scope-check", "--no-input")
	if err == nil || !strings.Contains(err.Error(), "--confirm") || calls != 0 {
		t.Fatal("destructive no-input operation confirmed by default")
	}
	if err := os.WriteFile(file, []byte("existing original intent"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err = executeCRMDocumentTest(t, "documents", "archive", crmDocumentTestID, "--data", crmDocumentExamples["archive"], "--idempotency-key", "original", "--intent-file", file, "--skip-scope-check", "--confirm", crmDocumentTestID)
	got, _ := os.ReadFile(file)
	if err == nil || calls != 0 || string(got) != "existing original intent" {
		t.Fatal("checkpoint overwritten or effect dispatched on collision")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(file, alias); err != nil {
		t.Fatal(err)
	}
	_, _, err = executeCRMDocumentTest(t, "documents", "archive", crmDocumentTestID, "--data", crmDocumentExamples["archive"], "--idempotency-key", "original", "--intent-file", alias, "--skip-scope-check", "--confirm", crmDocumentTestID)
	if err == nil || calls != 0 {
		t.Fatal("checkpoint symlink followed")
	}
}

func TestCRMDocumentsLiveAndScopePreflightUseNativeAuthority(t *testing.T) {
	for _, mode := range []string{"live", "missing-scope"} {
		t.Run(mode, func(t *testing.T) {
			calls, preflight := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v1/account" {
					preflight++
					fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm_documents:read"]}}}`)
					return
				}
				calls++
				fmt.Fprintf(w, `{"data":{"id":%q,"version":1}}`, crmDocumentTestID)
			}))
			defer server.Close()
			setupCRMDocumentTest(t, server.URL)
			if mode == "live" {
				t.Setenv("FACTUAREA_API_KEY", "fact_live_"+strings.Repeat("a", 24))
			}
			file := filepath.Join(t.TempDir(), "intent.json")
			_, _, err := executeCRMDocumentTest(t, "documents", "upload", "--data", crmDocumentExamples["upload"], "--idempotency-key", "original", "--intent-file", file)
			if err == nil || calls != 0 || (mode == "live" && preflight != 0) || (mode == "missing-scope" && preflight != 1) {
				t.Fatalf("native live/scope guard bypassed: %v", err)
			}
			if _, err := os.Stat(file); !os.IsNotExist(err) {
				t.Fatal("rejected preflight stored effect checkpoint")
			}
		})
	}
}

func TestCRMDocumentsPaginationPreservesOpaqueCursorAndExplicitLimits(t *testing.T) {
	for _, mode := range []string{"cursor", "cycle", "max-pages", "library", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if mode == "library" {
					fmt.Fprintf(w, `{"data":{"items":[{"id":%q,"version":1}],"total":2,"page":%s,"per_page":1}}`, crmDocumentTestID, r.URL.Query().Get("page"))
					return
				}
				if mode == "unavailable" {
					fmt.Fprintf(w, `{"data":{"opportunity_id":%q,"document_type":"contract","items":[],"next_cursor":null,"available":false,"unavailable_reason":"native_owner_off"}}`, crmDocumentTestID)
					return
				}
				cursor := `"Opaque +/=="`
				if calls > 1 {
					if r.URL.Query().Get("cursor") != "Opaque +/==" {
						t.Error("opaque cursor changed")
					}
					if mode != "cycle" {
						cursor = "null"
					}
				}
				fmt.Fprintf(w, `{"data":{"opportunity_id":%q,"items":[%s],"next_cursor":%s,"total":2,"pending_count":0}}`, crmDocumentTestID, crmDocumentTestLink("quote", false), cursor)
			}))
			defer server.Close()
			setupCRMDocumentTest(t, server.URL)
			args := []string{"opportunity-documents", "list", crmDocumentTestID, "--all", "--limit", "1", "--max-pages", "2", "--skip-scope-check"}
			if mode == "max-pages" {
				args[7] = "1"
			}
			if mode == "library" {
				args = []string{"documents", "list", "--all", "--per-page", "1", "--max-pages", "2", "--skip-scope-check"}
			}
			if mode == "unavailable" {
				args = []string{"opportunity-documents", "candidates", crmDocumentTestID, "--document-type", "contract", "--all", "--skip-scope-check"}
			}
			stdout, _, err := executeCRMDocumentTest(t, args...)
			if mode == "cycle" || mode == "max-pages" {
				if err == nil || !strings.Contains(err.Error(), "parcial") {
					t.Fatal("incomplete iteration claimed complete")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if mode == "max-pages" || mode == "unavailable" {
				if calls != 1 {
					t.Fatal("explicit limit ignored")
				}
			} else if calls != 2 {
				t.Fatal("cursor/pages not consumed")
			}
			if mode == "unavailable" && !strings.Contains(stdout, `"unavailable_reason":"native_owner_off"`) {
				t.Fatal("unavailable producer replaced by fake empty collection")
			}
			if strings.Count(stdout, "\n") != calls {
				t.Fatal("NDJSON native rows lost")
			}
		})
	}
}

func TestCRMDocumentsMaskedDecimalAndOwnerResponsesFailClosed(t *testing.T) {
	cases := []struct{ action, body string }{
		{"financial-summary", strings.Replace(crmDocumentTestResponse(crmDocumentOperations[13]), `"quoted":"999999999999999999.99"`, `"quoted":999999999999999999.99`, 1)},
		{"financial-summary", strings.Replace(crmDocumentTestResponse(crmDocumentOperations[13]), `"currency":"USD"`, `"currency":"EUR"`, 1)},
		{"financial-summary", strings.Replace(crmDocumentTestResponse(crmDocumentOperations[13]), crmDocumentTestID, crmDocumentTestOtherID, 1)},
		{"get", fmt.Sprintf(`{"data":{"id":%q,"version":1,"title":null}}`, crmDocumentTestID)},
		{"get", fmt.Sprintf(`{"data":{"id":%q,"version":1,"public_url":"https://public.invalid"}}`, crmDocumentTestID)},
		{"list", fmt.Sprintf(`{"data":{"opportunity_id":%q,"items":[],"next_cursor":null,"total":0,"pending_count":0}}`, crmDocumentTestOtherID)},
		{"candidates", fmt.Sprintf(`{"data":{"opportunity_id":%q,"document_type":"quote","items":[],"next_cursor":"fake","available":false,"unavailable_reason":"off"}}`, crmDocumentTestID)},
	}
	for index, c := range cases {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, c.body)
			}))
			defer server.Close()
			setupCRMDocumentTest(t, server.URL)
			group := "opportunity-documents"
			if c.action == "get" {
				group = "documents"
			}
			args := []string{group, c.action, crmDocumentTestID, "--skip-scope-check"}
			if c.action == "candidates" {
				args = append(args, "--document-type", "quote")
			}
			stdout, _, err := executeCRMDocumentTest(t, args...)
			if err == nil || stdout != "" {
				t.Fatal("invalid/masked/native owner response disclosed")
			}
		})
	}
}

func TestCRMDocumentsNativeLinkReplaysPrimaryAndNonFiscalNull(t *testing.T) {
	for _, kind := range []string{"quote", "invoice", "contract", "recurring_schedule"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(201)
				fmt.Fprint(w, `{"data":`+crmDocumentTestLink(kind, true)+`}`)
			}))
			defer server.Close()
			setupCRMDocumentTest(t, server.URL)
			body := fmt.Sprintf(`{"document_type":%q,"document_id":%q,"is_primary":false}`, kind, strings.ToUpper(crmDocumentTestNativeID))
			stdout, _, err := executeCRMDocumentTest(t, "opportunity-documents", "link", crmDocumentTestID, "--data", body, "--idempotency-key", "original", "--intent-file", filepath.Join(t.TempDir(), "intent.json"), "--skip-scope-check")
			if err != nil || !strings.Contains(stdout, `"is_primary":true`) {
				t.Fatal("native existing-primary replay incorrectly rejected")
			}
			if (kind == "contract" || kind == "recurring_schedule") && !strings.Contains(stdout, `"quoted_amount":null`) {
				t.Fatal("non-fiscal owner fabricated money")
			}
		})
	}
}

func TestCRMDocumentsHelpManifestAndSafeHumanOutput(t *testing.T) {
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
	operations, recovery := 0, 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Command, "factuarea crm documents ") || strings.HasPrefix(entry.Command, "factuarea crm opportunity-documents ") {
			if strings.HasSuffix(entry.Command, " recover") {
				recovery++
				if entry.OperationID != "" || entry.RequiredScope == "" {
					t.Fatal("recovery advertised fake endpoint")
				}
				continue
			}
			operations++
			if entry.RequiredScope == "" || entry.OperationID == "" {
				t.Fatal("native discovery missing scope/operation")
			}
		}
	}
	if operations != 14 || recovery != 2 {
		t.Fatalf("discovery mismatch %d/%d", operations, recovery)
	}
	t.Setenv("FACTUAREA_API_KEY", "")
	stdout, _, err := executeCRMDocumentTest(t, "opportunity-documents", "candidates", "--help")
	if err != nil || !strings.Contains(stdout, "recurring_schedule") {
		t.Fatal("local help missing native enum")
	}
	op := crmDocumentOperations[1]
	body := []byte(fmt.Sprintf(`{"data":{"id":%q,"version":1,"title":"Oferta\u001b[31m\nOtra"}}`, crmDocumentTestID))
	data, err := crmDocumentData(body)
	if err != nil {
		t.Fatal(err)
	}
	var table, raw bytes.Buffer
	if err := printCRMDocumentBody(&table, body, data, output.Human, op); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(table.String(), '\x1b') || !strings.Contains(table.String(), crmDocumentTestID) {
		t.Fatal("human output injected terminal control or lost UUID")
	}
	if err := printCRMDocumentBody(&raw, body, data, output.JSON, op); err != nil || raw.String() != string(body)+"\n" {
		t.Fatal("JSON output changed native bytes")
	}
	financial := []byte(crmDocumentTestResponse(crmDocumentOperations[13]))
	summary, _ := crmDocumentData(financial)
	table.Reset()
	if err := printCRMDocumentBody(&table, financial, summary, output.Human, crmDocumentOperations[13]); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(table.String(), "999999999999999999.9999") || !strings.Contains(table.String(), "USD") {
		t.Fatal("human output rounded/aggregated money")
	}
}
