package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

func TestCRMNativeCursorEnvelopesAndOpaqueFilters(t *testing.T) {
	for _, collection := range []string{"data", "items", "results"} {
		t.Run(collection, func(t *testing.T) {
			var cursors []string
			var filters []string
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				cursors = append(cursors, r.URL.Query().Get("cursor"))
				filters = append(filters, r.URL.Query().Get("filters"))
				if r.URL.Query().Get("starting_after") != "" {
					t.Error("invented legacy cursor")
				}
				if len(cursors) == 1 {
					fmt.Fprintf(w, `{"data":{"%s":[{"id":"019c0511-1111-7111-8111-111111111111"}],"has_more":true,"next_cursor":"signed+/opaque=="}}`, collection)
				} else {
					fmt.Fprintf(w, `{"data":{"%s":[{"id":"019c0511-1111-7111-8111-222222222222"}],"has_more":false,"next_cursor":null}}`, collection)
				}
			})
			plan := CRMPagination{Kind: "cursor", QueryParameter: "cursor", ItemsPath: "data." + collection, MorePath: "data.has_more", CursorPath: "data.next_cursor", IdentityPath: "id", MaxPages: 3}
			q := url.Values{"filters": {`{"score_min":0,"owner_id":"019c0511-1111-7111-8111-555555555555"}`}}
			var ids []string
			err := c.PaginateCRM(context.Background(), "/v1/crm/leads", q, plan, func(row json.RawMessage) error {
				var value map[string]string
				_ = json.Unmarshal(row, &value)
				ids = append(ids, value["id"])
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(ids, ",") != "019c0511-1111-7111-8111-111111111111,019c0511-1111-7111-8111-222222222222" || strings.Join(cursors, ",") != ",signed+/opaque==" {
				t.Fatalf("rows=%v cursors=%v", ids, cursors)
			}
			if filters[0] != filters[1] || q.Get("cursor") != "" {
				t.Fatalf("original filters or caller selection changed: %v %v", filters, q)
			}
		})
	}
}

func TestCRMNativeNumberedPagesUseCurrentMeta(t *testing.T) {
	var pages []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		pages = append(pages, r.URL.Query().Get("page"))
		if len(pages) == 1 {
			fmt.Fprint(w, `{"data":[{"id":"019c0511-1111-7111-8111-111111111111"}],"meta":{"current_page":1,"last_page":2,"total":2,"per_page":1}}`)
		} else {
			fmt.Fprint(w, `{"data":[{"id":"019c0511-1111-7111-8111-222222222222"}],"meta":{"current_page":2,"last_page":2,"total":2,"per_page":1}}`)
		}
	})
	plan := CRMPagination{Kind: "page", QueryParameter: "page", ItemsPath: "data", CurrentPagePath: "meta.current_page", LastPagePath: "meta.last_page", IdentityPath: "id", MaxPages: 3}
	count := 0
	err := c.PaginateCRM(context.Background(), "/v1/crm/contact-people", url.Values{"per_page": {"1"}}, plan, func(json.RawMessage) error { count++; return nil })
	if err != nil || count != 2 || strings.Join(pages, ",") != ",2" {
		t.Fatalf("count=%d pages=%v err=%v", count, pages, err)
	}
}

func TestCRMCursorRejectsIncompleteOrRepeatedContinuationBeforePagePublication(t *testing.T) {
	cases := []string{
		`{"data":{"data":[{"id":"019c0511-1111-7111-8111-111111111111"}],"has_more":true,"next_cursor":null}}`,
		`{"data":{"data":[{"id":"019c0511-1111-7111-8111-111111111111"}],"has_more":true,"next_cursor":"original"}}`,
		`{"data":{"data":[{"id":"019c0511-1111-7111-8111-111111111111"}],"has_more":null,"next_cursor":null}}`,
		`{"data":{"data":null,"has_more":false,"next_cursor":null}}`,
		`{"data":{"data":[{"id":"019c0511-1111-7111-8111-111111111111"},{"id":"019c0511-1111-7111-8111-111111111111"}],"has_more":false,"next_cursor":null}}`,
		`{"data":{"data":[{"name":"hidden identity"}],"has_more":false,"next_cursor":null}}`,
	}
	for i, body := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, body)
			})
			plan := CRMPagination{Kind: "cursor", QueryParameter: "cursor", ItemsPath: "data.data", MorePath: "data.has_more", CursorPath: "data.next_cursor", IdentityPath: "id", MaxPages: 2}
			published := 0
			err := c.PaginateCRM(context.Background(), "/v1/crm/leads", url.Values{"cursor": {"original"}}, plan, func(json.RawMessage) error { published++; return nil })
			var typed *apierr.APIError
			if !errors.As(err, &typed) || typed.Code != "invalid_crm_pagination" || published != 0 {
				t.Fatalf("published=%d error=%v", published, err)
			}
			if strings.Contains(err.Error(), "hidden identity") {
				t.Fatal("response data leaked")
			}
		})
	}
}

func TestCRMRevocationStopsBeforeAnotherAuthorizedPage(t *testing.T) {
	calls := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			fmt.Fprint(w, `{"data":{"items":[{"id":"019c0511-1111-7111-8111-333333333333"}],"has_more":true,"next_cursor":"opaque"}}`)
		} else {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"error":{"type":"authorization_error","code":"crm_context_revoked","message":"Contexto revocado"}}`)
		}
	})
	plan := CRMPagination{Kind: "cursor", QueryParameter: "cursor", ItemsPath: "data.items", MorePath: "data.has_more", CursorPath: "data.next_cursor", IdentityPath: "id", MaxPages: 4}
	published := 0
	err := c.PaginateCRM(context.Background(), "/v1/crm/pipelines", url.Values{}, plan, func(json.RawMessage) error { published++; return nil })
	var api *apierr.APIError
	if !errors.As(err, &api) || api.Code != "crm_context_revoked" || calls != 2 || published != 1 {
		t.Fatalf("calls=%d published=%d error=%v", calls, published, err)
	}
}

func TestCRMPageBudgetIsExplicitPartialTraversal(t *testing.T) {
	calls := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"data":[{"id":"019c0511-1111-7111-8111-333333333333"}],"has_more":true,"next_cursor":"next"}}`)
	})
	plan := CRMPagination{Kind: "cursor", QueryParameter: "cursor", ItemsPath: "data.data", MorePath: "data.has_more", CursorPath: "data.next_cursor", IdentityPath: "id", MaxPages: 1}
	published := 0
	err := c.PaginateCRM(context.Background(), "/v1/crm/leads", url.Values{}, plan, func(json.RawMessage) error { published++; return nil })
	var usage *apierr.UsageError
	if !errors.As(err, &usage) || !strings.Contains(err.Error(), "parcial") || calls != 1 || published != 1 {
		t.Fatalf("calls=%d published=%d error=%v", calls, published, err)
	}
}

func TestCRMNativeScoreRunKeepsLeadIdentity(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"results":[{"lead_id":"019c0511-1111-7111-8111-444444444444","outcome":"updated"}],"has_more":false,"next_cursor":null}}`)
	})
	plan := CRMPagination{Kind: "cursor", QueryParameter: "cursor", ItemsPath: "data.results", MorePath: "data.has_more", CursorPath: "data.next_cursor", IdentityPath: "lead_id", MaxPages: 1}
	var original json.RawMessage
	err := c.PaginateCRM(context.Background(), "/v1/crm/lead-score-runs/019c0511-1111-7111-8111-666666666666", url.Values{}, plan, func(row json.RawMessage) error { original = row; return nil })
	if err != nil || !strings.Contains(string(original), `"lead_id":"019c0511-1111-7111-8111-444444444444"`) || strings.Contains(string(original), `"id":`) {
		t.Fatalf("original=%s error=%v", original, err)
	}
}

func TestCRMCancelledTraversalMakesNoRequest(t *testing.T) {
	calls := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan := CRMPagination{Kind: "cursor", QueryParameter: "cursor", ItemsPath: "data.data", MorePath: "data.has_more", CursorPath: "data.next_cursor", IdentityPath: "id", MaxPages: 2}
	err := c.PaginateCRM(ctx, "/v1/crm/leads", url.Values{}, plan, func(json.RawMessage) error { return nil })
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}
