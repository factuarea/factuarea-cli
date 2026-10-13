package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/exit"
)

const serviceFormRefsQueueID = "019c0511-1111-7111-8111-333333333333"
const serviceFormRefsPolicyID = "019c0511-1111-7111-8111-444444444444"
const serviceFormRefsCalendarID = "019c0511-1111-7111-8111-555555555555"
const serviceFormRefsUserID = "550e8400-e29b-41d4-a716-446655440000"

func serviceFormRefsFixture() map[string]any {
	return map[string]any{"data": map[string]any{
		"queues":                []any{map[string]any{"id": serviceFormRefsQueueID, "name": "Soporte actual"}},
		"policies":              []any{map[string]any{"id": serviceFormRefsPolicyID, "name": "Acuerdo vigente"}},
		"calendars":             []any{map[string]any{"id": serviceFormRefsCalendarID, "name": "Calendario elegido", "version": json.Number("9007199254740993")}},
		"responsible_users":     []any{map[string]any{"id": serviceFormRefsUserID, "name": "Persona autorizada"}},
		"authorization_version": "current-company-c7-72",
	}}
}

func serviceFormRefsWire(t *testing.T, fixture map[string]any) string {
	t.Helper()
	wire, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	return string(wire)
}

func serviceFormRefsArgs(extra ...string) []string {
	return append([]string{"crm", "capture-forms", "service-form-references"}, extra...)
}

func TestServiceFormReferencesNativeReadPreservesConfiguredCredentialAndWire(t *testing.T) {
	wire := serviceFormRefsWire(t, serviceFormRefsFixture())
	var accountCalls, referencesCalls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fact_test_aaaaaaaaaaaaaaaaaaaaaaaa" {
			t.Error("configured staff credential changed")
		}
		if r.URL.Path == "/v1/account" {
			accountCalls.Add(1)
			crmAccount(w, serviceFormReferencesScope)
			return
		}
		referencesCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		if r.Method != "GET" || r.URL.Path != "/v1/crm/capture/service-form-references" || r.URL.RawQuery != "" || len(body) != 0 {
			t.Error("native closed GET changed")
		}
		for _, header := range []string{"Idempotency-Key", "X-Company-Id", "X-Tenant-Id", "Content-Type"} {
			if _, present := r.Header[header]; present || r.Header.Get(header) != "" {
				t.Errorf("invented header %s", header)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "private, no-store")
		_, _ = w.Write([]byte(wire))
	}))
	defer s.Close()
	out, stderr, err := crmRun(t, s, serviceFormRefsArgs("--json")...)
	if err != nil || accountCalls.Load() != 1 || referencesCalls.Load() != 1 || stderr != "" || strings.TrimSpace(out) != wire {
		t.Fatalf("native read changed: account=%d references=%d out=%q stderr=%q err=%v", accountCalls.Load(), referencesCalls.Load(), out, stderr, err)
	}
	if !strings.Contains(out, "9007199254740993") || strings.Contains(out, "\"uuid\"") || strings.Contains(out, "installation_key") {
		t.Fatal("native V1 identities/integer/projection changed")
	}
}

func TestServiceFormReferencesEmptyChoicesAndIndependentCalendars(t *testing.T) {
	for _, collections := range [][]string{
		{"queues", "policies", "calendars", "responsible_users"},
		{"policies"},
		{"calendars"},
	} {
		t.Run(strings.Join(collections, "+"), func(t *testing.T) {
			fixture := serviceFormRefsFixture()
			data := fixture["data"].(map[string]any)
			for _, collection := range collections {
				data[collection] = []any{}
			}
			wire := serviceFormRefsWire(t, fixture)
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, serviceFormReferencesScope)
					return
				}
				_, _ = w.Write([]byte(wire))
			}))
			defer s.Close()
			out, _, err := crmRun(t, s, serviceFormRefsArgs("--json")...)
			if err != nil || strings.TrimSpace(out) != wire {
				t.Fatalf("choices gained defaults or calendar dependency: %q %v", out, err)
			}
		})
	}
}

func TestServiceFormReferencesNativeCollectionAndIdentityBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		valid bool
	}{
		{"native maximum", 100, true},
		{"over native maximum", 101, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := serviceFormRefsFixture()
			data := fixture["data"].(map[string]any)
			options := []any{}
			for i := 0; i < tc.count; i++ {
				options = append(options, map[string]any{"id": fmt.Sprintf("019c0511-1111-7111-8111-%012x", i), "name": strings.Repeat("á", 150)})
			}
			data["queues"] = options
			data["responsible_users"].([]any)[0].(map[string]any)["id"] = serviceFormRefsQueueID
			// The producer does not couple policy and calendar identities. Retain
			// their distinct collections even when the UUID string is equal.
			data["policies"].([]any)[0].(map[string]any)["id"] = serviceFormRefsCalendarID
			data["calendars"].([]any)[0].(map[string]any)["version"] = json.Number("9223372036854775807")
			wire := serviceFormRefsWire(t, fixture)
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, serviceFormReferencesScope)
					return
				}
				_, _ = w.Write([]byte(wire))
			}))
			defer s.Close()
			out, _, err := crmRun(t, s, serviceFormRefsArgs("--json")...)
			if tc.valid && (err != nil || strings.TrimSpace(out) != wire) {
				t.Fatalf("native maximum/name/User UUID7/int64 boundary narrowed: %q %v", out, err)
			}
			if !tc.valid && (out != "" || exit.ForError(err) != exit.Server) {
				t.Fatalf("owner maximum bypassed: %q %v", out, err)
			}
		})
	}
}

func TestServiceFormReferencesCurrentScopeAndNoLocalGrants(t *testing.T) {
	for _, scopes := range [][]string{{"crm:read"}, {"customer_service:read", "service_level:read"}, {"crm_capture_forms.read"}, {"crm_capture_forms:write"}, {}} {
		t.Run(strings.Join(scopes, "+"), func(t *testing.T) {
			var referencesCalls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, scopes...)
					return
				}
				referencesCalls.Add(1)
			}))
			defer s.Close()
			out, _, err := crmRun(t, s, serviceFormRefsArgs("--json")...)
			if referencesCalls.Load() != 0 || out != "" || exit.ForError(err) != exit.Perm {
				t.Fatalf("invented read grant: calls=%d out=%q err=%v", referencesCalls.Load(), out, err)
			}
		})
	}
}

func TestServiceFormReferencesClosedInputRejectsBeforeNetwork(t *testing.T) {
	for _, extra := range [][]string{
		{"unexpected-id"}, {"--company-id", "42"}, {"--tenant", "42"}, {"--data", "{}"},
		{"--data-file", "form.json"}, {"--query", "status=active"}, {"--fields", "name"},
		{"--paginate"}, {"--cursor", "opaque"}, {"--idempotency-key", "old-original-key"},
		{"--skip-scope-check"}, {"--policy-id", serviceFormRefsPolicyID}, {"--calendar-id", serviceFormRefsCalendarID},
	} {
		t.Run(strings.Join(extra, " "), func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			defer s.Close()
			out, _, err := crmRun(t, s, serviceFormRefsArgs(extra...)...)
			if calls.Load() != 0 || out != "" || exit.ForError(err) != exit.Usage {
				t.Fatalf("closed input reached network: calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}

func TestServiceFormReferencesRejectsPrivateAliasesAndMalformedOwnerChoices(t *testing.T) {
	mutations := []struct {
		name string
		edit func(map[string]any)
	}{
		{"tenant carrier", func(d map[string]any) { d["company_id"] = 42 }},
		{"approval carrier", func(d map[string]any) { d["confirmed"] = true }},
		{"receipt carrier", func(d map[string]any) { d["effect_id"] = crmTestID }},
		{"missing owner collection", func(d map[string]any) { delete(d, "calendars") }},
		{"null owner collection", func(d map[string]any) { d["queues"] = nil }},
		{"object collection alias", func(d map[string]any) { d["policies"] = map[string]any{} }},
		{"missing authorization", func(d map[string]any) { delete(d, "authorization_version") }},
		{"null authorization", func(d map[string]any) { d["authorization_version"] = nil }},
		{"empty authorization", func(d map[string]any) { d["authorization_version"] = " \t\n" }},
		{"numeric authorization", func(d map[string]any) { d["authorization_version"] = 7 }},
		{"SPA identity alias", func(d map[string]any) {
			o := d["queues"].([]any)[0].(map[string]any)
			o["uuid"] = o["id"]
			delete(o, "id")
		}},
		{"UUID duplicate alias", func(d map[string]any) { o := d["queues"].([]any)[0].(map[string]any); o["uuid"] = o["id"] }},
		{"key alias", func(d map[string]any) {
			o := d["policies"].([]any)[0].(map[string]any)
			o["key"] = "private-policy-key"
		}},
		{"hidden queue team", func(d map[string]any) { d["queues"].([]any)[0].(map[string]any)["team_uuid"] = crmTestID }},
		{"implicit policy calendar", func(d map[string]any) {
			d["policies"].([]any)[0].(map[string]any)["calendar_id"] = serviceFormRefsCalendarID
		}},
		{"hidden calendar schedule", func(d map[string]any) { d["calendars"].([]any)[0].(map[string]any)["weekly_intervals"] = []any{} }},
		{"hidden responsible email", func(d map[string]any) {
			d["responsible_users"].([]any)[0].(map[string]any)["email"] = "private@example.test"
		}},
		{"historical queue UUID", func(d map[string]any) { d["queues"].([]any)[0].(map[string]any)["id"] = serviceFormRefsUserID }},
		{"uppercase UUID alias", func(d map[string]any) {
			d["policies"].([]any)[0].(map[string]any)["id"] = strings.ToUpper(serviceFormRefsPolicyID)
		}},
		{"invalid User UUID version", func(d map[string]any) {
			d["responsible_users"].([]any)[0].(map[string]any)["id"] = "550e8400-e29b-11d4-a716-446655440000"
		}},
		{"numeric identity", func(d map[string]any) { d["queues"].([]any)[0].(map[string]any)["id"] = 77 }},
		{"blank name", func(d map[string]any) { d["policies"].([]any)[0].(map[string]any)["name"] = " \t\r\n" }},
		{"long Unicode name", func(d map[string]any) { d["queues"].([]any)[0].(map[string]any)["name"] = strings.Repeat("á", 151) }},
		{"null name", func(d map[string]any) { d["responsible_users"].([]any)[0].(map[string]any)["name"] = nil }},
		{"missing calendar version", func(d map[string]any) { delete(d["calendars"].([]any)[0].(map[string]any), "version") }},
		{"zero calendar version", func(d map[string]any) { d["calendars"].([]any)[0].(map[string]any)["version"] = 0 }},
		{"string calendar version", func(d map[string]any) { d["calendars"].([]any)[0].(map[string]any)["version"] = "7" }},
		{"fraction calendar version", func(d map[string]any) { d["calendars"].([]any)[0].(map[string]any)["version"] = json.Number("7.5") }},
		{"overflow calendar version", func(d map[string]any) {
			d["calendars"].([]any)[0].(map[string]any)["version"] = json.Number("9223372036854775808")
		}},
	}
	for _, collection := range []string{"queues", "policies", "calendars", "responsible_users"} {
		mutations = append(mutations, struct {
			name string
			edit func(map[string]any)
		}{"duplicate " + collection, func(d map[string]any) {
			options := d[collection].([]any)
			original := options[0].(map[string]any)
			duplicate := map[string]any{}
			for k, v := range original {
				duplicate[k] = v
			}
			duplicate["name"] = "Nombre diferente"
			d[collection] = append(options, duplicate)
		}})
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			fixture := serviceFormRefsFixture()
			mutation.edit(fixture["data"].(map[string]any))
			wire := serviceFormRefsWire(t, fixture)
			var referencesCalls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, serviceFormReferencesScope)
					return
				}
				referencesCalls.Add(1)
				_, _ = w.Write([]byte(wire))
			}))
			defer s.Close()
			out, stderr, err := crmRun(t, s, serviceFormRefsArgs("--json")...)
			if referencesCalls.Load() != 1 || out != "" || exit.ForError(err) != exit.Server || !strings.Contains(stderr, "invalid_native_response") {
				t.Fatalf("unqualified response escaped: calls=%d out=%q stderr=%q err=%v", referencesCalls.Load(), out, stderr, err)
			}
			if strings.Contains(stderr, "private@example.test") || strings.Contains(stderr, "private-policy-key") {
				t.Fatal("private response leaked in validation error")
			}
		})
	}
}

func TestServiceFormReferencesRejectsAmbiguousJSONAndEnvelope(t *testing.T) {
	wire := serviceFormRefsWire(t, serviceFormRefsFixture())
	for _, invalid := range []string{
		`{"data":null}`, `{"data":[]}`, wire + `{}`, strings.Replace(wire, `"id":`, `"id":"overwritten","\u0069d":`, 1),
		strings.Replace(wire, `"data":`, `"data":null,"data":`, 1), `{"data":{"queues":[]}}`, wire[:len(wire)-1],
	} {
		t.Run(invalid[:min(len(invalid), 80)], func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, serviceFormReferencesScope)
					return
				}
				_, _ = w.Write([]byte(invalid))
			}))
			defer s.Close()
			out, _, err := crmRun(t, s, serviceFormRefsArgs("--json")...)
			if out != "" || exit.ForError(err) != exit.Server {
				t.Fatalf("ambiguous JSON shown: %q %v", out, err)
			}
		})
	}
}

func TestServiceFormReferencesNativeDenialsPreserveCurrentAuthorityAndNoFallback(t *testing.T) {
	for _, tc := range []struct {
		status int
		typeID string
		code   int
	}{
		{401, "authentication_error", exit.Auth}, {403, "authorization_error", exit.Perm},
		{404, "not_found_error", exit.NotFound}, {409, "conflict_error", exit.Conflict},
		{422, "invalid_request_error", exit.Validation}, {429, "rate_limit_error", exit.RateLimit},
		{503, "api_error", exit.Server},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			var referencesCalls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					crmAccount(w, serviceFormReferencesScope)
					return
				}
				referencesCalls.Add(1)
				if r.URL.Path != serviceFormReferencesPath {
					t.Error("invented fallback route")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"type": tc.typeID, "code": "current_owner_denied", "message": "El acceso actual no está disponible", "request_id": "references-request-72", "details": map[string]any{"reason": "current_c7"}}})
			}))
			defer s.Close()
			out, stderr, err := crmRun(t, s, serviceFormRefsArgs("--json")...)
			var native *apierr.APIError
			if referencesCalls.Load() != 1 || out != "" || exit.ForError(err) != tc.code || !errors.As(err, &native) || native.StatusCode != tc.status || native.Code != "current_owner_denied" || !strings.Contains(stderr, "references-request-72") || !strings.Contains(stderr, "current_c7") {
				t.Fatalf("native denial changed: calls=%d out=%q stderr=%q err=%v", referencesCalls.Load(), out, stderr, err)
			}
		})
	}
}

func TestServiceFormReferencesAccountRevocationNeverReadsOptions(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/account" {
			t.Error("read options after credential revocation")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","code":"credential_revoked","message":"Credencial revocada"}}`))
	}))
	defer s.Close()
	out, _, err := crmRun(t, s, serviceFormRefsArgs("--json")...)
	var native *apierr.APIError
	if calls.Load() != 1 || out != "" || exit.ForError(err) != exit.Auth || !errors.As(err, &native) || native.Code != "credential_revoked" {
		t.Fatalf("current credential denial bypassed: calls=%d err=%v", calls.Load(), err)
	}
}

func TestServiceFormReferencesReadonlyLiveCredentialUsesCurrentScope(t *testing.T) {
	var calls atomic.Int32
	wire := serviceFormRefsWire(t, serviceFormRefsFixture())
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fact_live_aaaaaaaaaaaaaaaaaaaaaaaa" {
			t.Error("configured LIVE credential changed")
		}
		if r.URL.Path == "/v1/account" {
			crmAccount(w, serviceFormReferencesScope)
			return
		}
		_, _ = w.Write([]byte(wire))
	}))
	defer s.Close()
	t.Setenv("FACTUAREA_API_KEY", "fact_live_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", s.URL)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs(serviceFormRefsArgs("--json"))
	if err := root.Execute(); err != nil || calls.Load() != 2 || strings.TrimSpace(out.String()) != wire {
		t.Fatalf("readonly operation gained LIVE mutation confirmation: calls=%d err=%v", calls.Load(), err)
	}
}

func TestServiceFormReferencesDiscoveryPreservesPriorNative111(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"commands", "--json"})
	if root.Execute() != nil {
		t.Fatal("discovery failed")
	}
	var entries []manifestEntry
	if json.Unmarshal(out.Bytes(), &entries) != nil {
		t.Fatal("invalid discovery JSON")
	}
	byID := map[string]manifestEntry{}
	for _, entry := range entries {
		if _, found := byID[entry.OperationID]; found {
			t.Fatalf("duplicate native operation %s", entry.OperationID)
		}
		byID[entry.OperationID] = entry
	}
	e, exists := byID[serviceFormReferencesID]
	if !exists || e.Command != serviceFormReferencesCommand || e.CrmOperation != serviceFormReferencesOperation || e.NativePath != serviceFormReferencesPath || e.Method != "GET" || e.AuthRealm != "staff" || e.OperationIDSource != "native_v1_surface" || e.RequiredScope != serviceFormReferencesScope || !reflect.DeepEqual(e.RequiredScopes, []string{serviceFormReferencesScope}) || len(e.Flags) != 0 || len(e.Args) != 0 || len(e.QuerySchemas) != 0 || e.RequestSchema != nil || e.ResponseSchemas["200"] == nil || e.Mutating || e.Paginated || e.IdempotencyRequired || e.Irreversible || e.RequiresConfirmation || e.LocalConfirmation || e.OriginalRecovery {
		t.Fatalf("references native manifest changed: %+v", e)
	}
	previous := append(nativeOwnerManifest(), taskTimeManifest()...)
	previous = append(previous, insightsManifest()...)
	if len(previous) != 34 {
		t.Fatal("previous explicit native operations changed")
	}
	for _, before := range previous {
		a, _ := json.Marshal(before)
		b, _ := json.Marshal(byID[before.OperationID])
		if !bytes.Equal(a, b) {
			t.Fatalf("previous native discovery changed: %s", before.OperationID)
		}
	}
	oldCRM := 0
	for _, before := range generatedOps() {
		e, exists := byID[before.OperationID]
		if !exists || e.Command != commandPath(before) || e.RequiredScope != before.RequiredScope || e.CrmOperation != before.CrmOperation {
			t.Fatalf("previous generated operation changed: %s", before.OperationID)
		}
		if before.CrmOperation != "" {
			oldCRM++
		}
	}
	if oldCRM != 77 {
		t.Fatalf("previous native cohort changed: %d", oldCRM)
	}
}

func TestServiceFormReferencesHelpAndDiscoveryStayOffline(t *testing.T) {
	for _, args := range [][]string{serviceFormRefsArgs("--help"), {"crm", "capture-forms", "--help"}, {"commands", "--json"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			defer s.Close()
			t.Setenv("FACTUAREA_API_KEY", "invalid-unused-credential")
			t.Setenv("FACTUAREA_BASE_URL", s.URL)
			root := NewRootCmd()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetArgs(args)
			if err := root.Execute(); err != nil || calls.Load() != 0 || !strings.Contains(out.String(), "service-form-references") {
				t.Fatalf("offline discovery required credential/network: %d %v", calls.Load(), err)
			}
		})
	}
}
