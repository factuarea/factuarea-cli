package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContactsListPreservesFiscalLeadAndPhoneFilters(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/account" {
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["contacts:read"]}}}`))
			return
		}
		calls++
		if r.Method != "GET" || r.URL.Path != "/v1/contacts" {
			t.Errorf("unexpected route: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("search") != "600123456" || strings.Join(r.URL.Query()["roles[]"], ",") != "lead,customer" {
			t.Errorf("filters lost: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"0199152d-525d-7000-8000-000000000001","name":"Prospect"}],"has_more":false,"next_cursor":null}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	root := NewRootCmd()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetArgs([]string{"contacts", "list", "--roles", "lead,customer", "--search", "600123456", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(output.String(), "Prospect") {
		t.Fatalf("missing contacts response: calls=%d output=%s", calls, output.String())
	}
}
