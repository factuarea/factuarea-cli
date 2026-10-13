package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/factuarea/factuarea-cli/internal/apierr"
	"strings"
	"testing"
)

func TestCRMDeliveryJSONUnconfirmedNeverPrintsCauseBodyOrCredential(t *testing.T) {
	var wire bytes.Buffer
	PrintError(&wire, &apierr.WriteUnconfirmed{Err: fmt.Errorf("private-token private-body"), Method: "POST", Path: "/v1/crm/leads", IdempotencyKey: "first-key", OriginalBodySHA256: strings.Repeat("a", 64)}, JSON)
	var payload struct {
		Error map[string]any `json:"error"`
	}
	if json.Unmarshal(wire.Bytes(), &payload) != nil || payload.Error["type"] != "write_unconfirmed" || payload.Error["state"] != "unconfirmed" || payload.Error["idempotency_key"] != "first-key" || strings.Contains(wire.String(), "private-token") || strings.Contains(wire.String(), "private-body") {
		t.Fatal("unconfirmed output changed or leaked")
	}
}
func TestCRMDeliveryJSONPreservesFieldsCASScopesAndRetryAfter(t *testing.T) {
	var wire bytes.Buffer
	PrintError(&wire, &apierr.APIError{StatusCode: 422, Details: map[string]any{"field_errors": map[string]any{"expected_version": []string{"stale"}}, "required_scope": "crm_leads:write"}, Errors: map[string]any{"title": []string{"required"}}, RetryAfter: "9"}, JSON)
	for _, part := range []string{`"http_status":422`, `"retry_after":"9"`, `"expected_version"`, `"required_scope":"crm_leads:write"`, `"title"`} {
		if !strings.Contains(wire.String(), part) {
			t.Fatalf("lost %s", part)
		}
	}
}
