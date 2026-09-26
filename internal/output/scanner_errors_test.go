package output

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

func TestScannerRejectionsAndReviewDetailsRemainMachineReadable(t *testing.T) {
	var out bytes.Buffer
	PrintError(&out, &apierr.APIError{
		StatusCode: 422, Code: "purchase_scan_review_incomplete", Message: "Review required",
		Data:    json.RawMessage(`{"accepted":[],"rejected":[{"item_index":0,"code":"scan_file_invalid"}]}`),
		Details: map[string]any{"field_errors": map[string]any{"supplier_id": []string{"Select a supplier"}}},
	}, JSON)
	var payload struct {
		Data struct {
			Rejected []struct {
				Code string `json:"code"`
			} `json:"rejected"`
		} `json:"data"`
		Error struct {
			Details struct {
				Fields map[string][]string `json:"field_errors"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Rejected) != 1 || payload.Data.Rejected[0].Code != "scan_file_invalid" {
		t.Fatalf("lost rejection: %s", out.String())
	}
	if len(payload.Error.Details.Fields["supplier_id"]) != 1 {
		t.Fatalf("lost review fields: %s", out.String())
	}
}
