package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestCRMDeliverySandboxTenantContextUsesSelectedTestCredential(t *testing.T) {
	type tenantFixture struct {
		apiKey        string
		companyID     string
		opportunityID string
	}
	fixtures := []tenantFixture{
		{
			apiKey:        "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa",
			companyID:     "01900000-0000-7000-8000-000000000001",
			opportunityID: "01900000-0000-7000-8000-000000000011",
		},
		{
			apiKey:        "fact_test_bbbbbbbbbbbbbbbbbbbbbbbb",
			companyID:     "01900000-0000-7000-8000-000000000002",
			opportunityID: "01900000-0000-7000-8000-000000000022",
		},
	}

	byKey := make(map[string]tenantFixture, len(fixtures))
	for _, fixture := range fixtures {
		byKey[fixture.apiKey] = fixture
	}
	requestsByKey := make(map[string]int, len(fixtures))
	var requestsMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		apiKey := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		fixture, ok := byKey[apiKey]
		if !ok {
			http.Error(writer, `{"error":{"message":"unknown fixture credential"}}`, http.StatusUnauthorized)
			return
		}
		requestsMu.Lock()
		requestsByKey[apiKey]++
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
				"api_key":    map[string]any{"scopes": []string{"crm_opportunities:read"}},
			}})
			return
		}
		if request.URL.Path != "/v1/crm/opportunity" {
			http.Error(writer, `{"error":{"message":"unexpected CRM route"}}`, http.StatusNotFound)
			return
		}
		if request.URL.Query().Get("company_id") != "" {
			http.Error(writer, `{"error":{"message":"client company override is forbidden"}}`, http.StatusBadRequest)
			return
		}
		if request.URL.Query().Get("filters") != `{"status":"open"}` || request.URL.Query().Get("cursor") != "opaque+/=cursor" || request.URL.Query().Get("limit") != "25" {
			http.Error(writer, `{"error":{"message":"CRM filters or cursor changed"}}`, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"data": map[string]any{
			"items":       []map[string]string{{"id": fixture.opportunityID, "company_id": fixture.companyID}},
			"next_cursor": nil,
		}})
	}))
	defer server.Close()

	for _, fixture := range fixtures {
		t.Run(fixture.companyID, func(t *testing.T) {
			t.Setenv(envBaseURL, server.URL)
			t.Setenv("FACTUAREA_API_KEY", fixture.apiKey)
			t.Setenv("FACTUAREA_PROFILE", "")

			stdout, stderr, err := executeCRMDelivery(t,
				"--json", "crm-opportunities", "list",
				"--filters", `{"status":"open"}`,
				"--cursor", "opaque+/=cursor",
				"--limit", "25",
			)
			if err != nil {
				t.Fatalf("CRM read failed: %v; stderr=%s", err, stderr)
			}
			if !strings.Contains(stdout, fixture.companyID) || !strings.Contains(stdout, fixture.opportunityID) {
				t.Fatalf("read returned another tenant's fixture: %s", stdout)
			}
			for _, other := range fixtures {
				if other.apiKey != fixture.apiKey && (strings.Contains(stdout, other.companyID) || strings.Contains(stdout, other.opportunityID)) {
					t.Fatalf("read leaked another sandbox tenant: %s", stdout)
				}
			}
			if strings.Contains(stdout+stderr, fixture.apiKey) {
				t.Fatal("CLI output disclosed the sandbox credential")
			}
			requestsMu.Lock()
			got := requestsByKey[fixture.apiKey]
			requestsMu.Unlock()
			if got != 2 {
				t.Fatalf("credential made %d requests; want account preflight and one CRM read", got)
			}
		})
	}
	totalRequests := 0
	requestsMu.Lock()
	for _, fixture := range fixtures {
		totalRequests += requestsByKey[fixture.apiKey]
	}
	requestsMu.Unlock()
	if totalRequests != 4 {
		t.Fatalf("unexpected total fixture requests: %d; want one account preflight and one CRM read per tenant", totalRequests)
	}
}
