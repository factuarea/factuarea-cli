package spec

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCRMNativeInventoryPreservesOriginalOperationIDsAndScopes(t *testing.T) {
	var published struct {
		Paths map[string]map[string]struct {
			ID      string `json:"operationId"`
			Native  string `json:"x-crm-operation"`
			Scope   string `json:"x-required-scope"`
			Confirm bool   `json:"x-crm-requires-confirmation"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(Raw, &published); err != nil {
		t.Fatal(err)
	}
	ops, nonConforming, err := Load()
	if err != nil || len(nonConforming) > 0 {
		t.Fatalf("load=%v nonconforming=%v", err, nonConforming)
	}
	parsed := map[string]Operation{}
	for _, op := range ops {
		parsed[op.OperationID] = op
	}
	counts := map[string]int{}
	for path, item := range published.Paths {
		for method, wire := range item {
			family, _, _ := strings.Cut(wire.Native, ".")
			if family != "crm_contact_people" && family != "crm_leads" && family != "crm_pipelines" && family != "knowledge_articles" && family != "public_help_centers" {
				continue
			}
			op, ok := parsed[wire.ID]
			if !ok {
				t.Fatalf("original operation %s is absent", wire.ID)
			}
			if op.OperationID != wire.ID || op.Method != strings.ToUpper(method) || op.Path != path || op.RequiredScope != wire.Scope || op.CrmOperation != wire.Native {
				t.Fatalf("original public contract changed: %+v wire=%+v", op, wire)
			}
			if wire.Confirm && !op.Irreversible {
				t.Fatalf("native human confirmation missing for %s", wire.ID)
			}
			counts[family]++
		}
	}
	for family, want := range map[string]int{"crm_contact_people": 11, "crm_leads": 23, "crm_pipelines": 15, "knowledge_articles": 23, "public_help_centers": 5} {
		if counts[family] != want {
			t.Errorf("%s: actual%d expected%d; reconcile native inventory before delivery", family, counts[family], want)
		}
	}
}

func TestCRMNativePaginationReadsDeclaredEnvelopes(t *testing.T) {
	ops, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	byNative := map[string]Operation{}
	for _, op := range ops {
		byNative[op.CrmOperation] = op
	}
	for native, want := range map[string]Pagination{
		"crm_contact_people.index":               {Kind: "page", QueryParameter: "page", ItemsPath: "data", CurrentPagePath: "meta.current_page", LastPagePath: "meta.last_page", IdentityPath: "id"},
		"crm_leads.index":                        {Kind: "cursor", QueryParameter: "cursor", ItemsPath: "data.data", MorePath: "data.has_more", CursorPath: "data.next_cursor", IdentityPath: "id"},
		"crm_leads.history":                      {Kind: "cursor", QueryParameter: "cursor", ItemsPath: "data.data", MorePath: "data.has_more", CursorPath: "data.next_cursor", IdentityPath: "id"},
		"crm_pipelines.index":                    {Kind: "cursor", QueryParameter: "cursor", ItemsPath: "data.items", MorePath: "data.has_more", CursorPath: "data.next_cursor", IdentityPath: "id"},
		"crm_leads.find_score_recalculation_run": {Kind: "cursor", QueryParameter: "cursor", ItemsPath: "data.results", MorePath: "data.has_more", CursorPath: "data.next_cursor", IdentityPath: "lead_id"},
	} {
		t.Run(native, func(t *testing.T) {
			op := byNative[native]
			if op.Pagination == nil || *op.Pagination != want {
				t.Fatalf("native paging mismatch: got%+v expected%+v", op.Pagination, want)
			}
		})
	}
	if byNative["crm_contact_people.duplicates"].Pagination != nil {
		t.Fatal("duplicates does not publish last_page; automatic traversal must remain unavailable")
	}
}

func TestCRMNativeUUIDPatternsSurviveGeneration(t *testing.T) {
	ops, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, op := range ops {
		if op.CrmOperation == "crm_leads.show" {
			found = true
			if len(op.PathParams) != 1 || op.PathParams[0].Format != "uuid" || !strings.Contains(op.PathParams[0].Pattern, "-7") {
				t.Fatalf("native public UUIDv7 contract lost: %+v", op.PathParams)
			}
		}
	}
	if !found {
		t.Fatal("native lead show operation is absent")
	}
}
