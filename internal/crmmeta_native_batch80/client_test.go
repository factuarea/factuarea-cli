package crmmeta_native_batch80

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const testConnection = "01900000-0000-7000-8000-000000000001"
const testForm = "01900000-0000-7000-8000-000000000002"
const testCredential = "01900000-0000-4000-8000-000000000003"
const testPage = "90071992547409931234567890123456"
const testState = "01900000-0000-7000-8000-000000000004"

func testMappings() []Mapping {
	return []Mapping{{RemoteName: "Àngela 山", Target: "name", Type: "text", Required: true, Options: map[string]string{}}}
}
func testInspect() InspectInput {
	return InspectInput{testCredential, "12345678901234567890123456789012"}
}
func testPublish() MappingInput {
	return MappingInput{CredentialUUID: testCredential, RemoteFormID: testInspect().RemoteFormID, Mapping: testMappings(), ExpectedPublicationVersion: math.MaxInt64 - 1, FieldsSnapshotChecksum: strings.Repeat("a", 64), Confirmed: true}
}
func testPublication() MappingPublication {
	return MappingPublication{Status: "mapping_published", ConnectionUUID: testConnection, CredentialUUID: testCredential, FormUUID: testForm, PageID: testPage, RemoteFormID: testInspect().RemoteFormID, MappingVersion: 4, PublicationVersion: math.MaxInt64 - 1, FieldsSnapshotChecksum: strings.Repeat("a", 64), Mapping: testMappings()}
}
func testInspection() Inspection {
	return Inspection{Status: "mapping_inspected", ConnectionUUID: testConnection, CredentialUUID: testCredential, FormUUID: testForm, PageID: testPage, RemoteFormID: testInspect().RemoteFormID, AppID: "123", ResponsibleUUID: testCredential, RemoteFieldNames: []string{"Àngela 山"}, PublicationVersion: 9, AuthorizationVersion: 2, ConfigurationVersion: 3, FieldsSnapshotChecksum: strings.Repeat("a", 64), Destination: Destination{BasicFields: []string{"name", "notes"}, RequiredBasicFields: []string{"name"}, AllowedFieldIDs: []string{testForm}, RequiredFieldIDs: []string{}, FieldsSnapshot: json.RawMessage(`{"z":[{"version":7,"options":[{"id":"` + testForm + `","name":"Original"}]}],"a":{"required":true}}`)}, AppReview: AppReview{Reason: "app_review_evidence_unavailable"}}
}
func testWire(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func testClient(t *testing.T, server string) *Client {
	t.Helper()
	c, e := NewClient(Config{OAuthBearer: "actual-native-token", BaseURL: server + "/v1", Version: "2026-10-10"})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestSevenActualCarriersNoAutomaticHumanFollowUp(t *testing.T) {
	responses := []any{OAuthPreparation{"https://www.facebook.com/dialog/oauth", testState}, OAuthInstallation{"credentials_installed", testConnection, testCredential, testPage, []string{"ads_management", "leads_retrieval", "pages_manage_ads", "pages_manage_metadata", "pages_read_engagement", "pages_show_list"}}, map[string]any{"data": ConnectionOptions{[]ConnectionOption{{testConnection, testCredential, testPage, nil}}}}, map[string]any{"data": testPublication()}, map[string]any{"data": testInspection()}, map[string]any{"data": testPublication()}, map[string]string{"error": "meta_oauth_unavailable"}}
	var paths, methods, auths, bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := len(paths)
		wire, _ := io.ReadAll(r.Body)
		paths = append(paths, r.URL.Path)
		methods = append(methods, r.Method)
		auths = append(auths, r.Header.Get("Authorization"))
		bodies = append(bodies, string(wire))
		if r.URL.RawQuery != "" || r.Header.Get("Idempotency-Key") != "" || r.Header.Get("X-API-Key") != "" || r.Header.Get("Factuarea-Version") != "2026-10-10" {
			t.Error("native transport gained fabricated input")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "native-request")
		if n == 6 {
			w.WriteHeader(503)
		}
		w.Write(testWire(t, responses[n]))
	}))
	defer server.Close()
	c := testClient(t, server.URL)
	ctx := context.Background()
	prep, e := c.PrepareCrmMetaOAuth(ctx, PrepareInput{testPage, true})
	if e != nil || prep.Data.(OAuthPreparation).State != testState || len(paths) != 1 {
		t.Fatal("preparation must not complete itself", e)
	}
	install, e := c.CompleteCrmMetaOAuth(ctx, CompleteInput{testState, "actual-human-code"})
	if e != nil || !reflect.DeepEqual(install.Data.(OAuthInstallation).Scopes, []string{"ads_management", "leads_retrieval", "pages_manage_ads", "pages_manage_metadata", "pages_read_engagement", "pages_show_list"}) {
		t.Fatal(e)
	}
	if _, e = c.ListCrmMetaConnectionOptions(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = c.GetCrmMetaMapping(ctx, testConnection, testForm); e != nil {
		t.Fatal(e)
	}
	inspected, e := c.InspectCrmMetaMapping(ctx, testConnection, testForm, testInspect())
	if e != nil {
		t.Fatal(e)
	}
	actual := inspected.Data.(Inspection)
	if !bytes.Equal(actual.Destination.FieldsSnapshot, testInspection().Destination.FieldsSnapshot) || actual.AppReview.Available || actual.ExpectedMappingVersion != nil || actual.ExpectedConsentVersion != nil {
		t.Fatal("original Fields/CAS/Review changed")
	}
	if _, e = c.PublishCrmMetaMapping(ctx, testConnection, testForm, testPublish()); e != nil {
		t.Fatal(e)
	}
	_, e = c.PublishCrmMetaConsent(ctx, testConnection, ConsentInput{testCredential, []Grant{{testForm, []string{"read", "ingest"}}}, nil, true})
	var native *MetaError
	if !errors.As(e, &native) || native.Status != 503 || native.Reason != "unavailable" || native.Unconfirmed {
		t.Fatal("current Review must remain unavailable", e)
	}
	expected := []string{"/oauth/prepare", "/oauth/complete", "/options", "/" + testConnection + "/mappings/" + testForm, "/" + testConnection + "/mappings/" + testForm + "/inspect", "/" + testConnection + "/mappings/" + testForm, "/" + testConnection + "/consent"}
	for n, s := range expected {
		if paths[n] != "/v1"+prefix+s || auths[n] != "Bearer actual-native-token" {
			t.Fatal(paths, auths)
		}
	}
	if !reflect.DeepEqual(methods, []string{"POST", "POST", "GET", "GET", "POST", "PUT", "POST"}) || bodies[2] != "" || bodies[3] != "" || !strings.Contains(bodies[5], `"options":{}`) || !strings.Contains(bodies[5], `9223372036854775806`) {
		t.Fatal(methods, bodies)
	}
}

type testRoundTrip func(*http.Request) (*http.Response, error)

func (f testRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestExactFirstWireCurrentOAuthAndNoReconstruction(t *testing.T) {
	token := "native-parent"
	raw := []byte(" \n{\"confirmed\":true,\"page_id\":\"" + testPage + "\"}\n")
	intent, e := NewIntent(PrepareOAuth, "", "", raw)
	if e != nil {
		t.Fatal(e)
	}
	raw[0] = 'x'
	sent := 0
	c, e := NewClient(Config{OAuthToken: func(context.Context) (string, error) { return token, nil }, HTTPClient: &http.Client{Transport: testRoundTrip(func(r *http.Request) (*http.Response, error) {
		sent++
		body, _ := io.ReadAll(r.Body)
		if !bytes.Equal(body, intent.FirstWire()) || r.GetBody != nil || r.Header.Get("Authorization") != "Bearer authentic-current-descendant" {
			t.Error("first wire/current native token changed")
		}
		return nil, errors.New("secret-code-and-state")
	})}})
	if e != nil {
		t.Fatal(e)
	}
	token = "authentic-current-descendant"
	_, e = c.Execute(context.Background(), intent)
	var native *MetaError
	if !errors.As(e, &native) || native.Reason != "unconfirmed" || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
	_, e = c.Execute(context.Background(), intent)
	if !errors.As(e, &native) || native.Reason != "already_dispatched" || sent != 1 {
		t.Fatal("replayed", sent, e)
	}
}
func TestNativeErrorsRedirectsAndRateLimitAreOneDispatch(t *testing.T) {
	for _, status := range []int{401, 409, 429, 503, 307} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			count := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count++
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "7")
				w.Header().Set("Location", "https://foreign.invalid")
				w.WriteHeader(status)
				io.WriteString(w, `{"error":"meta_oauth_unconfirmed","provider_body":"secret-code-and-state"}`)
			}))
			defer server.Close()
			_, e := testClient(t, server.URL).CompleteCrmMetaOAuth(context.Background(), CompleteInput{testState, "secret-code-and-state"})
			var native *MetaError
			if !errors.As(e, &native) || native.Status != status || native.RetryAfter == nil || *native.RetryAfter != 7 || strings.Contains(e.Error(), "secret") || count != 1 {
				t.Fatal(count, e)
			}
		})
	}
}
func TestInspectPostNetworkFailureIsQuery(t *testing.T) {
	sent := 0
	c, e := NewClient(Config{APIKey: "fact_test_actual", HTTPClient: &http.Client{Transport: testRoundTrip(func(r *http.Request) (*http.Response, error) {
		sent++
		if r.Header.Get("Authorization") != "Bearer fact_test_actual" {
			t.Error("API key guise")
		}
		return nil, errors.New("secret")
	})}})
	if e != nil {
		t.Fatal(e)
	}
	_, e = c.InspectCrmMetaMapping(context.Background(), testConnection, testForm, testInspect())
	var native *MetaError
	if !errors.As(e, &native) || native.Unconfirmed || native.Reason != "transport_failure" || sent != 1 {
		t.Fatal(e)
	}
}
func TestClosedInputsCASAndActualDictionary(t *testing.T) {
	valid := testWire(t, testPublish())
	cases := [][]byte{
		bytes.Replace(valid, []byte(`"confirmed":true`), []byte(`"confirmed":false`), 1), bytes.Replace(valid, []byte(`"expected_mapping_version":null`), []byte(`"expected_mapping_version":0`), 1), bytes.Replace(valid, []byte(`9223372036854775806`), []byte(`9223372036854775807`), 1), bytes.Replace(valid, []byte(`"options":{}`), []byte(`"options":[]`), 1), bytes.Replace(valid, []byte(`"expected_mapping_version":null,`), nil, 1), append(append([]byte{}, valid[:len(valid)-1]...), []byte(`,"company_id":"`+testConnection+`"}`)...),
	}
	for n, raw := range cases {
		t.Run(string(rune('A'+n)), func(t *testing.T) {
			if _, e := NewIntent(PublishMapping, testConnection, testForm, raw); e == nil {
				t.Fatal("accepted fabricated CAS/context/options")
			}
		})
	}
	if _, e := NewIntent(PublishMapping, testConnection, testForm, valid); e != nil {
		t.Fatal(e)
	}
}
func TestForeignBindingAndInventedReviewStayInvalidQuery(t *testing.T) {
	bad := []Inspection{testInspection(), testInspection(), testInspection()}
	bad[0].CredentialUUID = testConnection
	bad[1].FormUUID = testConnection
	bad[2].AppReview = AppReview{true, "approved"}
	for _, v := range bad {
		i, e := NewIntent(Inspect, testConnection, testForm, testWire(t, testInspect()))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = projection(i, testWire(t, map[string]any{"data": v})); e == nil {
			t.Fatal("accepted foreign/invented authority")
		}
	}
}
func TestActualOperationIDsScopesAndAuthenticationModels(t *testing.T) {
	want := map[Action]string{PrepareOAuth: "prepareCrmMetaOAuth", CompleteOAuth: "completeCrmMetaOAuth", Options: "listCrmMetaConnectionOptions", GetMapping: "getCrmMetaMapping", Inspect: "inspectCrmMetaMapping", PublishMapping: "publishCrmMetaMapping", PublishConsent: "publishCrmMetaConsent"}
	for a, id := range want {
		if Routes()[a].OperationID != id {
			t.Fatal(a)
		}
	}
	if Operation != "crm_connections.configure" || FineScope != "crm_connections:write" || OAuthScope != "crm_connections.write" || Routes()[Inspect].Mutating {
		t.Fatal("native catalog changed")
	}
	for _, c := range []Config{{OAuthBearer: "fact_test_actual"}, {APIKey: "fact_test_actual", OAuthBearer: "native"}, {OAuthBearer: "native", OAuthToken: func(context.Context) (string, error) { return "other", nil }}} {
		if _, e := NewClient(c); e == nil {
			t.Fatal("accepted fabricated/ambiguous auth")
		}
	}
}
func TestNativeCodeBytesAndActualMetaPermissionUnion(t *testing.T) {
	if _, e := NewIntent(CompleteOAuth, "", "", testWire(t, CompleteInput{testState, strings.Repeat("é", 4097)})); e == nil {
		t.Fatal("accepted code over the original 8192-byte boundary")
	}
	i, e := NewIntent(CompleteOAuth, "", "", testWire(t, CompleteInput{testState, "actual-code"}))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = projection(i, testWire(t, OAuthInstallation{"credentials_installed", testConnection, testCredential, testPage, []string{"leads_retrieval"}})); e == nil {
		t.Fatal("accepted installation without actual native Meta permission union")
	}
}
func TestActualC7CodesClosedWithoutProviderSecretStrings(t *testing.T) {
	for code, reason := range map[string]string{"crm_access_denied": "crm_access_denied", "secret_code_and_state": "native_rejection"} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(403)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": "secret_code_and_state"}})
		}))
		_, e := testClient(t, server.URL).ListCrmMetaConnectionOptions(context.Background())
		server.Close()
		var native *MetaError
		if !errors.As(e, &native) || native.Status != 403 || native.Reason != reason || calls != 1 || strings.Contains(e.Error(), "secret_code_and_state") {
			t.Fatal(e, calls)
		}
	}
}
