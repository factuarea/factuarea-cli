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
	"path/filepath"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

const crmOAuthTestID = "01900000-0000-7000-8000-000000000001"
const crmOAuthFirstBody = " \n{\"amount\":\"999999999999999999.99\",\"name\":\"Àngela 山\",\"optional\":null}\n"
const crmOAuthOriginalBody = " { \"data\": { \"amount\": \"999999999999999999.99\", \"optional\": null } }\n"

func crmOAuthTestDigest() string {
	sum := sha256.Sum256([]byte(crmOAuthFirstBody))
	return hex.EncodeToString(sum[:])
}
func executeCRMOAuthTest(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd()
	var output, stderr bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"--json", "crm"}, args...))
	err := root.Execute()
	return output.String(), err
}
func setupCRMOAuthTest(t *testing.T, base string) string {
	t.Helper()
	t.Setenv("FACTUAREA_BASE_URL", base)
	t.Setenv("CURRENT_OAUTH_ACCESS_TOKEN", "native-parent")
	t.Setenv("FACTUAREA_API_KEY", "fact_test_"+strings.Repeat("a", 24))
	path := filepath.Join(t.TempDir(), "first-body.json")
	if err := os.WriteFile(path, []byte(crmOAuthFirstBody), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func crmOAuthTestReceipt(state, digest string) string {
	var original any
	if state == "confirmed" {
		original = map[string]any{"status": 201, "body": crmOAuthOriginalBody}
	}
	wire, _ := json.Marshal(map[string]any{"data": map[string]any{"state": state, "original_body_sha256": digest, "original_response": original}, "meta": map[string]any{"authorization_version": "native-current"}})
	return string(wire)
}
func TestCRMOAuthFourNativeOriginalGETsExactBytesAndKey(t *testing.T) {
	for _, op := range crmOAuthOriginalOperations {
		t.Run(op.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				path := strings.ReplaceAll(op.path, "{opportunity}", crmOAuthTestID)
				if r.Method != "GET" || r.URL.Path != path || r.Header.Get("Authorization") != "Bearer native-parent" || r.Header.Get("Idempotency-Key") != "raw original key" || r.Header.Get("X-API-Key") != "" {
					t.Errorf("request changed native original identity: %s %s", r.Method, r.URL.Path)
				}
				if body, _ := io.ReadAll(r.Body); len(body) != 0 {
					t.Error("recovery sent body")
				}
				if query := r.URL.Query(); len(query) != 1 || query.Get("original_body_sha256") != crmOAuthTestDigest() {
					t.Error("original hash/query changed")
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, crmOAuthTestReceipt("confirmed", crmOAuthTestDigest()))
			}))
			defer server.Close()
			first := setupCRMOAuthTest(t, server.URL)
			args := []string{"originals", op.name}
			if op.stage {
				args = append(args, strings.ToUpper(crmOAuthTestID))
			}
			args = append(args, "--first-body-file", first, "--idempotency-key", "raw original key", "--oauth-token-env", "CURRENT_OAUTH_ACCESS_TOKEN")
			wire, err := executeCRMOAuthTest(t, args...)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Errorf("calls=%d", calls)
			}
			var payload struct {
				Data crmOAuthOriginalReceipt `json:"data"`
			}
			if err := json.Unmarshal([]byte(wire), &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Data.Original == nil || *payload.Data.Original.Body != crmOAuthOriginalBody {
				t.Error("original response body was reserialized")
			}
			body, _ := os.ReadFile(first)
			if string(body) != crmOAuthFirstBody {
				t.Error("first body changed")
			}
		})
	}
}
func TestCRMOAuthCredentialContextUsesRealBearerWithoutAPIKeyAccountPreflight(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/v1/crm/credential-context" || r.Header.Get("Authorization") != "Bearer native-parent" || r.URL.RawQuery != "" {
			t.Error("not actual native context GET")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":{"company_id":%q,"credential_id":%q,"client_id":%q,"environment":"test","scopes":["crm_access_context:read","contacts:delete","invoices:send","recurring_invoices:transition","*"],"authorization_version":"native-current"}}`, crmOAuthTestID, crmOAuthTestID, crmOAuthTestID)
	}))
	defer server.Close()
	setupCRMOAuthTest(t, server.URL)
	wire, err := executeCRMOAuthTest(t, "credential-context", "--oauth-token-env", "CURRENT_OAUTH_ACCESS_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(wire, "crm_access_context:read") {
		t.Error("missing real context")
	}
	t.Setenv("CURRENT_OAUTH_ACCESS_TOKEN", "fact_test_"+strings.Repeat("a", 24))
	if _, err := executeCRMOAuthTest(t, "credential-context", "--oauth-token-env", "CURRENT_OAUTH_ACCESS_TOKEN"); err == nil {
		t.Error("API key masqueraded as OAuth")
	}
	if calls != 1 {
		t.Error("invalid OAuth credential dispatched")
	}
}
func TestCRMOAuthCurrentDescendantRecoveryAndAPIKeySingleton(t *testing.T) {
	var credentials []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("automatic mutation")
		}
		credentials = append(credentials, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, crmOAuthTestReceipt("missing", crmOAuthTestDigest()))
	}))
	defer server.Close()
	first := setupCRMOAuthTest(t, server.URL)
	args := []string{"originals", "lead-create", "--first-body-file", first, "--idempotency-key", "key"}
	for _, token := range []string{"native-parent", "authentic-descendant"} {
		t.Setenv("CURRENT_OAUTH_ACCESS_TOKEN", token)
		if _, err := executeCRMOAuthTest(t, append(args, "--oauth-token-env", "CURRENT_OAUTH_ACCESS_TOKEN")...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := executeCRMOAuthTest(t, args...); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(credentials, "|"); got != "Bearer native-parent|Bearer authentic-descendant|Bearer fact_test_"+strings.Repeat("a", 24) {
		t.Error("native credential sources mixed")
	}
}
func TestCRMOAuthFailuresNeverRefreshRetryRedirectOrWrite(t *testing.T) {
	for _, status := range []int{401, 429, 503, 307} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" {
					t.Error("mutation replayed")
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Location", "/another")
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":{"message":"revoked","code":"revoked"}}`)
			}))
			defer server.Close()
			first := setupCRMOAuthTest(t, server.URL)
			_, err := executeCRMOAuthTest(t, "originals", "lead-create", "--first-body-file", first, "--idempotency-key", "key", "--oauth-token-env", "CURRENT_OAUTH_ACCESS_TOKEN")
			api, ok := err.(*apierr.APIError)
			if !ok || api.StatusCode != status || calls != 1 {
				t.Errorf("failure not preserved once: %v calls=%d", err, calls)
			}
		})
	}
}
func TestCRMOAuthNonconfirmedAndMalformedReceipt(t *testing.T) {
	for _, state := range []string{"pending", "in_progress", "unconfirmed", "missing", "unknown"} {
		t.Run(state, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, crmOAuthTestReceipt(state, crmOAuthTestDigest()))
			}))
			defer server.Close()
			first := setupCRMOAuthTest(t, server.URL)
			wire, err := executeCRMOAuthTest(t, "originals", "lead-create", "--first-body-file", first, "--idempotency-key", "key", "--oauth-token-env", "CURRENT_OAUTH_ACCESS_TOKEN")
			if state == "unknown" {
				if err == nil {
					t.Fatal("accepted malformed state")
				}
			} else if err != nil || !strings.Contains(wire, `"original_response":null`) {
				t.Errorf("unconfirmed state changed: %v", err)
			}
			if calls != 1 {
				t.Error("unconfirmed read caused retry")
			}
		})
	}
}
func TestCRMOAuthCancelledGETAndNormalizedKeyFailClosed(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		t.Error("cancelled or malformed request dispatched")
	}))
	defer server.Close()
	first := setupCRMOAuthTest(t, server.URL)
	if _, err := executeCRMOAuthTest(t, "originals", "lead-create", "--first-body-file", first, "--idempotency-key", " key ", "--oauth-token-env", "CURRENT_OAUTH_ACCESS_TOKEN"); err == nil {
		t.Fatal("normalized original key accepted")
	}
	root := NewRootCmd()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root.SetContext(ctx)
	root.SetArgs([]string{"--json", "crm", "originals", "lead-create", "--first-body-file", first, "--idempotency-key", "key", "--oauth-token-env", "CURRENT_OAUTH_ACCESS_TOKEN"})
	if err := root.Execute(); err == nil {
		t.Fatal("cancelled original GET confirmed")
	}
	if calls != 0 {
		t.Error("cancelled GET dispatched")
	}
}
func TestCRMOAuthDiscoveryAndHelpDescribeOnlyReadRecovery(t *testing.T) {
	entries := crmOAuthOriginalManifest()
	if len(entries) != 5 {
		t.Fatal("missing native recovery commands")
	}
	for _, entry := range entries {
		if entry.Mutating || entry.RequiredScope == "" || entry.OperationID == "" {
			t.Error("discovery changed recovery contract")
		}
	}
	help, err := executeCRMOAuthTest(t, "originals", "lead-create", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, term := range []string{"--first-body-file", "--idempotency-key", "--oauth-token-env", "crm_leads.write"} {
		if !strings.Contains(help, term) {
			t.Errorf("help missing %s", term)
		}
	}
}
