package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/exit"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/spf13/cobra"
)

func executeCRMDelivery(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func executeCRMDeliveryInput(t *testing.T, input string, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetIn(strings.NewReader(input))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func crmDeliveryEnv(t *testing.T, base string) {
	t.Helper()
	t.Setenv(envBaseURL, base)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_"+strings.Repeat("a", 24))
	t.Setenv("FACTUAREA_PROFILE", "")
}

func TestCRMDeliveryCoreDiscoveryUsesActualRoutesWithoutDuplicateGeneratedOperations(t *testing.T) {
	ops := crmCoreNativeOps()
	if len(ops) != 16 {
		t.Fatalf("operations=%d", len(ops))
	}
	if len(crmCoreMissingOps(ops)) != 0 {
		t.Fatal("regenerated routes registered twice")
	}
	root := NewRootCmd()
	for _, op := range ops {
		leaf, _, err := root.Find(append(op.Groups, op.Action))
		if err != nil || leaf.Name() != op.Action {
			t.Fatalf("missing real command %s", commandPath(op))
		}
	}
	wire, _, err := executeCRMDelivery(t, "commands", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []manifestEntry
	if json.Unmarshal([]byte(wire), &entries) != nil {
		t.Fatal("invalid discovery")
	}
	for _, op := range ops {
		actual := op
		for _, generated := range generatedOps() {
			if generated.Method == op.Method && strings.TrimPrefix(generated.Path, "/v1") == op.Path {
				actual = generated
				break
			}
		}
		count := 0
		for _, entry := range entries {
			if entry.Command == commandPath(actual) && entry.OperationID == actual.OperationID {
				count++
				if entry.RequiredScope != op.RequiredScope {
					t.Fatal("scope changed")
				}
			}
		}
		if count != 1 {
			t.Fatalf("%s count=%d", actual.OperationID, count)
		}
	}
}

func TestCRMDeliveryRawJSONValidationPreservesExactDecimalsNullUnicodeAndWhitespace(t *testing.T) {
	wire := []byte(" \n{\"amount\":999999999999999999.9999,\"name\":\"Àngela 山\",\"optional\":null}\n")
	actual, err := validateRawJSONBody(wire, true)
	if err != nil || !bytes.Equal(actual, wire) {
		t.Fatal("raw request reconstructed")
	}
	for _, invalid := range []string{`{} {}`, `[]`, `null`, "{\"name\":\"\xff\"}"} {
		if _, err := validateRawJSONBody([]byte(invalid), true); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}

func TestCRMDeliveryDryRunAndHelpRequireNoCredentialOrHTTP(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	t.Setenv(envBaseURL, server.URL)
	t.Setenv("FACTUAREA_API_KEY", "")
	t.Setenv("FACTUAREA_PROFILE", "")
	wire, _, err := executeCRMDelivery(t, "crm-opportunities", "create", "--dry-run", "-d", `{"title":"Preview","amount":"9000000000000000.0001","currency":"EUR"}`)
	if err != nil || !strings.Contains(wire, `"9000000000000000.0001"`) {
		t.Fatal("native dry-run needed a credential")
	}
	help, _, err := executeCRMDelivery(t, "crm-leads", "create", "--help")
	if err != nil || !strings.Contains(help, "first-body-output") || !strings.Contains(help, "crm_leads:write") {
		t.Fatal("help missing actual recovery/scopes")
	}
	if calls != 0 {
		t.Fatalf("help or dry-run made %d HTTP requests", calls)
	}
}

func TestCRMDeliveryDryRunRawSourcesPreserveWireWithoutCreatingAnAttempt(t *testing.T) {
	wire := " \n{\"title\":\"Àngela 山\",\"amount\":9000000000000000.0001,\"optional\":null}\n"
	inputFile := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(inputFile, []byte(wire), 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	t.Setenv(envBaseURL, server.URL)
	t.Setenv("FACTUAREA_API_KEY", "fact_live_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_PROFILE", "crm-delivery-dry-run-missing")
	for _, source := range []struct {
		name, stdin string
		flags       []string
	}{
		{name: "inline", flags: []string{"-d", wire}},
		{name: "stdin", stdin: wire, flags: []string{"-d", "-"}},
		{name: "at_file", flags: []string{"-d", "@" + inputFile}},
		{name: "data_file", flags: []string{"--data-file", inputFile}},
	} {
		t.Run(source.name, func(t *testing.T) {
			first := filepath.Join(t.TempDir(), "first-body.json")
			args := append([]string{"--json", "crm-opportunities", "create", "--dry-run", "--first-body-output", first}, source.flags...)
			stdout, stderr, err := executeCRMDeliveryInput(t, source.stdin, args...)
			if err != nil || stderr != "" || stdout != wire+"\n" || calls.Load() != 0 {
				t.Fatalf("dry-run source %q changed bytes or resolved credentials/HTTP: err=%v stderr=%s calls=%d", source.name, err, stderr, calls.Load())
			}
			if _, err := os.Stat(first); !os.IsNotExist(err) {
				t.Fatal("dry-run created a retained write attempt")
			}
		})
	}
	first := filepath.Join(t.TempDir(), "invalid-first-body.json")
	stdout, _, err := executeCRMDeliveryInput(t, `{"title":"private-preview"} {}`, "--json", "crm-opportunities", "create", "--dry-run", "--first-body-output", first, "-d", "-")
	var usage *apierr.UsageError
	if !errors.As(err, &usage) || exit.ForError(err) != 2 || stdout != "" || calls.Load() != 0 {
		t.Fatal("malformed dry-run reached credentials/HTTP or emitted a preview")
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatal("invalid dry-run retained a write attempt")
	}
}

func TestCRMDeliveryNativeListHelpDocumentsFiltersAndOpaqueCursorWithoutHTTP(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	t.Setenv(envBaseURL, server.URL)
	t.Setenv("FACTUAREA_API_KEY", "")
	t.Setenv("FACTUAREA_PROFILE", "")

	help, stderr, err := executeCRMDelivery(t, "crm-opportunities", "list", "--help")
	if err != nil || stderr != "" || calls != 0 {
		t.Fatalf("native list help required credentials or HTTP: err=%v stderr=%s calls=%d", err, stderr, calls)
	}
	for _, expected := range []string{"--filters", "--cursor", "--limit", "--all", "--max-pages", "cursor opaco"} {
		if !strings.Contains(help, expected) {
			t.Fatalf("native list help missing %q: %s", expected, help)
		}
	}
}

func TestCRMDeliveryNativeSinglePagePreservesOpaqueCursorFiltersAndLimitWithoutPrefetch(t *testing.T) {
	const filters = ` {"status":"open","search":"Àngela 山 +/="} `
	const cursor = "https://cursor.example.test/private?opaque=+/= 山"
	const next = "next opaque +/= cursor"
	for _, fixture := range []struct {
		group, action, path, scope, rows string
	}{
		{"crm-leads", "index", "/v1/crm/leads", "crm_leads:read", "data"},
		{"crm-opportunities", "list", "/v1/crm/opportunity", "crm_opportunities:read", "items"},
		{"crm-activities", "index", "/v1/crm/crmactivity", "crm_activities:read", "items"},
	} {
		t.Run(fixture.group, func(t *testing.T) {
			var accountReads, pageReads atomic.Int32
			body := fmt.Sprintf(` {"data":{"%s":[{"id":"01900000-0000-7000-8000-000000000001","amount":9000000000000000.0001,"optional":null}],"next_cursor":%q,"has_more":true}} `, fixture.rows, next)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer fact_test_aaaaaaaaaaaaaaaaaaaaaaaa" {
					t.Error("native read changed method or credential")
				}
				writer.Header().Set("Content-Type", "application/json")
				if request.URL.Path == "/v1/account" {
					accountReads.Add(1)
					fmt.Fprintf(writer, `{"data":{"api_key":{"scopes":[%q]}}}`, fixture.scope)
					return
				}
				pageReads.Add(1)
				query := request.URL.Query()
				if request.URL.Path != fixture.path || query.Get("filters") != filters || query.Get("cursor") != cursor || query.Get("limit") != "25" || len(query) != 3 {
					t.Error("single native page changed route, opaque input or public query")
				}
				fmt.Fprint(writer, body)
			}))
			defer server.Close()
			crmDeliveryEnv(t, server.URL)
			stdout, stderr, err := executeCRMDelivery(t, "--json", fixture.group, fixture.action, "--filters", filters, "--cursor", cursor, "--limit", "25")
			if err != nil || stderr != "" || accountReads.Load() != 1 || pageReads.Load() != 1 || stdout != body+"\n" {
				t.Fatalf("default read prefetched or changed the complete native page: err=%v account=%d pages=%d", err, accountReads.Load(), pageReads.Load())
			}
		})
	}
}

func TestCRMDeliveryAmbiguousCreateRecoversActualFirstWireOnlyWithGET(t *testing.T) {
	wire := []byte(" \n{\"name\":\"Àngela 山\",\"notes\":null}\n")
	digest := sha256.Sum256(wire)
	sha := hex.EncodeToString(digest[:])
	writes, reads := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm_leads:write"]}}}`)
			return
		}
		if r.Method == "POST" {
			writes++
			actual, _ := io.ReadAll(r.Body)
			if !bytes.Equal(actual, wire) || r.Header.Get("Idempotency-Key") != "first key" {
				t.Error("wire/key changed")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":{"message":"ambiguous"}}`)
			return
		}
		reads++
		if r.URL.Path != "/v1/crm/leads/create/original" || r.URL.Query().Get("original_body_sha256") != sha || r.Header.Get("Idempotency-Key") != "first key" {
			t.Error("recovery changed identity")
		}
		w.Header().Set("Content-Type", "application/json")
		original := ` { "data": { "id": "01900000-0000-7000-8000-000000000001", "representation_available":false } } `
		response, _ := json.Marshal(map[string]any{"data": map[string]any{"state": "confirmed", "original_body_sha256": sha, "original_response": map[string]any{"status": 201, "body": original}}, "meta": map[string]any{"authorization_version": "current-server"}})
		w.Write(response)
	}))
	defer server.Close()
	crmDeliveryEnv(t, server.URL)
	first := filepath.Join(t.TempDir(), "actual-first-wire.json")
	_, stderr, err := executeCRMDelivery(t, "--json", "crm-leads", "create", "--idempotency-key", "first key", "--first-body-output", first, "-d", string(wire))
	var unknown *apierr.WriteUnconfirmed
	if !errors.As(err, &unknown) || exit.ForError(err) != 11 || writes != 1 || reads != 0 || !strings.Contains(stderr, sha) {
		t.Fatalf("ambiguous write was retried or lost: %v", err)
	}
	actual, err := os.ReadFile(first)
	if err != nil || !bytes.Equal(actual, wire) {
		t.Fatal("first wire not preserved")
	}
	info, _ := os.Stat(first)
	if info.Mode().Perm() != 0600 {
		t.Fatal("first wire not private")
	}
	response, _, err := executeCRMDelivery(t, "--json", "crm", "originals", "lead-create", "--idempotency-key", "first key", "--first-body-file", first)
	if err != nil || writes != 1 || reads != 1 || !strings.Contains(response, `"state":"confirmed"`) {
		t.Fatal("GET original did not recover same write")
	}
}

func TestCRMDeliveryOriginalDigestInputIsExclusiveAndNeverRebuildsBody(t *testing.T) {
	calls := 0
	digest := strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Query().Get("original_body_sha256") != digest {
			t.Error("not actual original GET")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, crmOAuthTestReceipt("missing", digest))
	}))
	defer server.Close()
	crmDeliveryEnv(t, server.URL)
	_, _, err := executeCRMDelivery(t, "--json", "crm", "originals", "activity-create", "--original-body-sha256", digest, "--idempotency-key", "same-key")
	if err != nil || calls != 1 {
		t.Fatal("digest recovery failed")
	}
	_, _, err = executeCRMDelivery(t, "crm", "originals", "activity-create", "--original-body-sha256", digest, "--first-body-file", "not-read", "--idempotency-key", "same-key")
	if err == nil || calls != 1 {
		t.Fatal("mixed identity accepted")
	}
}

func TestCRMDeliveryNativeCursorTraversalIsExplicitBoundedAndKeepsFilters(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm_opportunities:read"]}}}`)
			return
		}
		calls++
		if r.URL.Query().Get("filters") != `{"status":"open"}` {
			t.Error("filters changed")
		}
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			fmt.Fprint(w, `{"data":{"items":[{"id":"first","amount":"9000000000000000.0001"}],"next_cursor":"opaque+/=cursor"}}`)
		} else {
			if r.URL.Query().Get("cursor") != "opaque+/=cursor" {
				t.Error("cursor was inferred/changed")
			}
			fmt.Fprint(w, `{"data":{"items":[],"next_cursor":null}}`)
		}
	}))
	defer server.Close()
	crmDeliveryEnv(t, server.URL)
	wire, _, err := executeCRMDelivery(t, "--json", "crm-opportunities", "list", "--filters", `{"status":"open"}`, "--all", "--max-pages", "2")
	if err != nil || calls != 2 || !strings.Contains(wire, `"9000000000000000.0001"`) {
		t.Fatal("native pages not preserved")
	}
	_, _, err = executeCRMDelivery(t, "crm-opportunities", "list", "--all", "--max-pages", "0")
	if err == nil || calls != 2 {
		t.Fatal("invalid budget issued HTTP")
	}
}

func TestCRMDeliverySelectedProfilesKeepSandboxCompanyReadsIsolated(t *testing.T) {
	type tenantFixture struct {
		profile, apiKey, companyID, opportunityID string
	}
	fixtures := []tenantFixture{
		{
			profile:       "crm-delivery-sandbox-a",
			apiKey:        "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa",
			companyID:     "01900000-0000-7000-8000-000000000001",
			opportunityID: "01900000-0000-7000-8000-000000000011",
		},
		{
			profile:       "crm-delivery-sandbox-b",
			apiKey:        "fact_test_bbbbbbbbbbbbbbbbbbbbbbbb",
			companyID:     "01900000-0000-7000-8000-000000000002",
			opportunityID: "01900000-0000-7000-8000-000000000022",
		},
	}
	// This third credential stays in the generic environment. A selected
	// profile must win over it, even when it belongs to another sandbox company.
	genericKey := "fact_test_cccccccccccccccccccccccc"
	allFixtures := append(append([]tenantFixture{}, fixtures...), tenantFixture{
		apiKey:        genericKey,
		companyID:     "01900000-0000-7000-8000-000000000003",
		opportunityID: "01900000-0000-7000-8000-000000000033",
	})
	byKey := make(map[string]tenantFixture, len(allFixtures))
	for _, fixture := range allFixtures {
		byKey[fixture.apiKey] = fixture
	}
	requestsByKey := make(map[string]map[string]int, len(allFixtures))
	var requestsMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		apiKey := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		fixture, ok := byKey[apiKey]
		if !ok {
			http.Error(writer, `{"error":{"message":"unknown fixture credential"}}`, http.StatusUnauthorized)
			return
		}
		requestsMu.Lock()
		if requestsByKey[apiKey] == nil {
			requestsByKey[apiKey] = make(map[string]int)
		}
		requestsByKey[apiKey][request.URL.Path]++
		requestsMu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		if request.Method != http.MethodGet {
			http.Error(writer, `{"error":{"message":"unexpected method"}}`, http.StatusMethodNotAllowed)
			return
		}
		if request.URL.Path == "/v1/account" {
			_ = json.NewEncoder(writer).Encode(map[string]any{"data": map[string]any{
				"company_id":  fixture.companyID,
				"environment": "test",
				"api_key":     map[string]any{"scopes": []string{"crm_opportunities:read"}},
			}})
			return
		}
		if request.URL.Path != "/v1/crm/opportunity" || request.URL.Query().Get("company_id") != "" {
			http.Error(writer, `{"error":{"message":"unexpected CRM route or company override"}}`, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"data": map[string]any{
			"items":       []map[string]string{{"id": fixture.opportunityID, "company_id": fixture.companyID}},
			"next_cursor": nil,
		}})
	}))
	defer server.Close()

	t.Setenv(envBaseURL, server.URL)
	t.Setenv("FACTUAREA_API_KEY", genericKey)
	t.Setenv("FACTUAREA_PROFILE", "")
	for _, fixture := range fixtures {
		_, stderr, err := executeCRMDeliveryInput(t, fixture.apiKey+"\n", "--profile", fixture.profile, "--no-input", "login", "--api-key", "-")
		if err != nil {
			t.Fatalf("profile %q login failed: %v; stderr=%s", fixture.profile, err, stderr)
		}
		if strings.Contains(stderr, fixture.apiKey) {
			t.Fatalf("profile %q login disclosed its credential", fixture.profile)
		}
	}

	for _, fixture := range fixtures {
		t.Run(fixture.profile, func(t *testing.T) {
			stdout, stderr, err := executeCRMDelivery(t,
				"--profile", fixture.profile,
				"--json", "crm-opportunities", "list",
				"--filters", `{"status":"open"}`,
			)
			if err != nil {
				t.Fatalf("selected sandbox read failed: %v; stderr=%s", err, stderr)
			}
			if !strings.Contains(stdout, fixture.companyID) || !strings.Contains(stdout, fixture.opportunityID) {
				t.Fatalf("profile %q returned another sandbox company's data: %s", fixture.profile, stdout)
			}
			for _, other := range allFixtures {
				if other.apiKey != fixture.apiKey && (strings.Contains(stdout, other.companyID) || strings.Contains(stdout, other.opportunityID)) {
					t.Fatalf("profile %q exposed company %s: %s", fixture.profile, other.companyID, stdout)
				}
			}
			if strings.Contains(stdout+stderr, fixture.apiKey) || strings.Contains(stdout+stderr, genericKey) {
				t.Fatalf("profile %q disclosed a credential", fixture.profile)
			}
		})
	}

	_, _, err := executeCRMDelivery(t,
		"--profile", "crm-delivery-missing",
		"--json", "crm-opportunities", "list",
	)
	if err == nil || exit.ForError(err) != 2 {
		t.Fatal("missing selected profile fell back to the generic sandbox credential")
	}

	requestsMu.Lock()
	defer requestsMu.Unlock()
	for _, fixture := range fixtures {
		if got := requestsByKey[fixture.apiKey]["/v1/account"]; got != 2 {
			t.Fatalf("profile %q used /v1/account %d times; want login validation and read preflight", fixture.profile, got)
		}
		if got := requestsByKey[fixture.apiKey]["/v1/crm/opportunity"]; got != 1 {
			t.Fatalf("profile %q dispatched %d CRM reads; want 1", fixture.profile, got)
		}
	}
	if got := requestsByKey[genericKey]["/v1/account"] + requestsByKey[genericKey]["/v1/crm/opportunity"]; got != 0 {
		t.Fatalf("selected profiles crossed to the generic credential (%d requests)", got)
	}
}

func TestCRMDeliverySelectedScopeDenialAndCASHaveStableExitCodes(t *testing.T) {
	for _, status := range []int{403, 409, 422, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm_opportunities:write"]}}}`)
					return
				}
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "17")
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":{"message":"denied","details":{"field_errors":{"expected_version":["stale"]},"required_scope":"crm_opportunities:write"}}}`)
			}))
			defer server.Close()
			crmDeliveryEnv(t, server.URL)
			_, stderr, err := executeCRMDelivery(t, "--json", "crm-opportunities", "move-stage", crmOAuthTestID, "-d", `{"expected_version":1,"expected_definition_version":0,"target_pipeline_id":"01900000-0000-7000-8000-000000000001","target_stage_id":"01900000-0000-7000-8000-000000000001"}`)
			want := map[int]int{403: 4, 409: 8, 422: 5, 429: 7}[status]
			if err == nil || exit.ForError(err) != want || calls != 1 || !strings.Contains(stderr, "expected_version") || !strings.Contains(stderr, `"retry_after":"17"`) {
				t.Fatalf("typed error lost %v", err)
			}
		})
	}
}

func TestCRMDeliveryCASConflictRedactsCredentialFromEveryPrintedErrorField(t *testing.T) {
	apiKey := "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa"
	const wire = `{"expected_version":1,"expected_definition_version":0,"target_pipeline_id":"01900000-0000-7000-8000-000000000001","target_stage_id":"01900000-0000-7000-8000-000000000001"}`
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/account" {
			fmt.Fprint(writer, `{"data":{"api_key":{"scopes":["crm_opportunities:write"]}}}`)
			return
		}
		calls++
		body, err := io.ReadAll(request.Body)
		if err != nil || string(body) != wire || request.Method != http.MethodPost || request.URL.Path != "/v1/crm/opportunity/"+crmOAuthTestID+"/stage" || request.Header.Get("Idempotency-Key") != "crm-delivery-cas-first" {
			t.Error("CAS changed the original versions, body, path or attempt identity")
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Retry-After", apiKey)
		writer.Header().Set("X-Request-Id", apiKey)
		writer.WriteHeader(http.StatusConflict)
		fmt.Fprintf(writer,
			`{"error":{"type":"owner_conflict","message":"stale %s","subcode":"%s","param":"%s","doc_url":"https://docs.example.test/?token=%s","details":{"field_errors":{"expected_version":["%s"],"required_scope":"crm_opportunities:write"}},"request_id":"%s"},"errors":{"target_stage_id":["%s"]},"data":{"diagnostic":"%s"}}`,
			apiKey, apiKey, apiKey, apiKey, apiKey, apiKey, apiKey, apiKey,
		)
	}))
	defer server.Close()
	t.Setenv(envBaseURL, server.URL)
	t.Setenv("FACTUAREA_API_KEY", apiKey)
	t.Setenv("FACTUAREA_PROFILE", "")

	_, stderr, err := executeCRMDelivery(t,
		"--json", "crm-opportunities", "move-stage", crmOAuthTestID, "--idempotency-key", "crm-delivery-cas-first", "-d", wire,
	)
	if err == nil || exit.ForError(err) != 8 || calls != 1 {
		t.Fatalf("CAS conflict was not preserved as one definitive 409: %v", err)
	}
	if strings.Contains(stderr+err.Error(), apiKey) {
		t.Fatal("credential leaked through the rendered or returned conflict")
	}
	var unknown *apierr.WriteUnconfirmed
	if errors.As(err, &unknown) || !json.Valid([]byte(stderr)) {
		t.Fatal("definitive CAS conflict became an unconfirmed write or invalid JSON")
	}
	for _, field := range []string{"expected_version", "required_scope", "target_stage_id", `"http_status":409`, `"retry_after":"[REDACTED]"`, `"request_id":"[REDACTED]"`, `"subcode":"[REDACTED]"`, `"param":"[REDACTED]"`, `"diagnostic":"[REDACTED]"`} {
		if !strings.Contains(stderr, field) {
			t.Fatalf("typed conflict field %q was lost: %s", field, stderr)
		}
	}
	var human bytes.Buffer
	output.PrintError(&human, err, output.Human)
	if strings.Contains(human.String(), apiKey) || !strings.Contains(human.String(), "[REDACTED]") || !strings.Contains(human.String(), "docs.example.test") {
		t.Fatal("human conflict rendering disclosed a bearer or lost native diagnostics")
	}
}

func TestCRMDeliveryPrivateFirstWireNeverOverwritesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "first.json")
	if err := os.WriteFile(path, []byte(`{"first":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveCRMFirstWire(path, []byte(`{"second":true}`)); err == nil {
		t.Fatal("original overwritten")
	}
	actual, _ := os.ReadFile(path)
	if string(actual) != `{"first":true}` {
		t.Fatal("original changed")
	}
}

func TestCRMDeliveryScopePreflightDenialNeverWritesOrUsesForeignUUID(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/account" {
			writes++
			t.Error("denied operation dispatched")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm_leads:read"]}}}`)
	}))
	defer server.Close()
	crmDeliveryEnv(t, server.URL)
	_, _, err := executeCRMDelivery(t, "--json", "crm-leads", "create", "-d", `{"name":"No grant"}`)
	if err == nil || exit.ForError(err) != 4 || writes != 0 {
		t.Fatal("read key implied write scope")
	}
	_, _, err = executeCRMDelivery(t, "crm-leads", "show", "01900000-0000-4000-8000-000000000001")
	if err == nil || exit.ForError(err) != 2 || writes != 0 {
		t.Fatal("invalid public UUID dispatched")
	}
}

func TestCRMDeliveryPathUUIDRequiresV7BeforeHTTPAndAcceptsUppercase(t *testing.T) {
	var accountReads, resourceReads int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/v1/account" {
			accountReads++
			fmt.Fprint(writer, `{"data":{"api_key":{"scopes":["crm_opportunities:read"]}}}`)
			return
		}
		resourceReads++
		if request.Method != http.MethodGet || request.URL.Path != "/v1/crm/opportunity/01900000-0000-7000-8000-00000000000A" {
			t.Errorf("valid uppercase UUID changed or reached another route: %s %s", request.Method, request.URL.Path)
		}
		fmt.Fprint(writer, `{"data":{"id":"01900000-0000-7000-8000-00000000000A","status":"open"}}`)
	}))
	defer server.Close()
	crmDeliveryEnv(t, server.URL)

	invalid := []string{
		"not-a-uuid",
		"01900000-0000-4000-8000-000000000001", // UUIDv4
		"01900000-0000-7000-7000-000000000001", // invalid RFC variant
		"../01900000-0000-7000-8000-000000000001",
	}
	for _, id := range invalid {
		t.Run("reject_"+id, func(t *testing.T) {
			_, _, err := executeCRMDelivery(t, "--json", "crm-opportunities", "show", id)
			if err == nil || exit.ForError(err) != 2 || accountReads != 0 || resourceReads != 0 {
				t.Fatalf("invalid public id reached credential preflight or HTTP: %v", err)
			}
		})
	}

	stdout, stderr, err := executeCRMDelivery(t, "--json", "crm-opportunities", "show", "01900000-0000-7000-8000-00000000000A")
	if err != nil || stderr != "" || accountReads != 1 || resourceReads != 1 || !strings.Contains(stdout, `"id":"01900000-0000-7000-8000-00000000000A"`) {
		t.Fatalf("valid uppercase UUIDv7 was not preserved: out=%s err=%v", stdout, err)
	}
}

func TestCRMDeliveryBaseURLRejectsCredentialAndQueryBeforeTransport(t *testing.T) {
	for _, base := range []string{"https://user:private-bearer@example.test", "https://example.test?token=private-bearer", "https://example.test#private-bearer"} {
		if err := validateBaseURLTransport(base, false); err == nil || strings.Contains(err.Error(), "private-bearer") {
			t.Fatal("invalid credential boundary accepted or disclosed")
		}
	}
}

func TestCRMDeliveryAllBudgetRetainsNextCursorWithoutRepeatingPage(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			fmt.Fprint(w, `{"data":{"api_key":{"scopes":["crm_activities:read"]}}}`)
			return
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"items":[],"next_cursor":"next-private-cursor"}}`)
	}))
	defer server.Close()
	crmDeliveryEnv(t, server.URL)
	_, _, err := executeCRMDelivery(t, "--json", "crm-activities", "index", "--all", "--max-pages", "1")
	var api *apierr.APIError
	if !errors.As(err, &api) || api.Code != "pagination_budget_exhausted" || api.Details.(map[string]any)["next_cursor"] != "next-private-cursor" || calls != 1 {
		t.Fatal("budget loop or continuation lost")
	}
}

func TestCRMDeliveryHumanReceiptPreservesActualActivityIdentityAndConfirmedState(t *testing.T) {
	command := &cobra.Command{}
	var wire bytes.Buffer
	command.SetOut(&wire)
	op := genOp{Method: "POST", Path: "/crm/crmactivity"}
	if err := printCRMCoreBody(command, op, []byte(`{"data":{"state":"confirmed","effect_id":"native-effect","activity_id":"native-activity","activity":null}}`), output.Human); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wire.String(), "native-activity") || !strings.Contains(wire.String(), "confirmed") || strings.Contains(wire.String(), "activity:null") {
		t.Fatal("native receipt was invented or obscured")
	}
}
