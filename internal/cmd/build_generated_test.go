package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeneratedListCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/invoices" {
			t.Errorf("path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"inv_1"}],"has_more":false,"next_cursor":null}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"invoices", "list", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), `"id":"inv_1"`) {
		t.Fatalf("salida: %s", out.String())
	}
}

func TestGeneratedMutatingInheritsLiveGuard(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "fact_live_bbbbbbbbbbbbbbbbbbbbbbbb")
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"invoices", "create", "-d", "{}"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "LIVE") {
		t.Fatalf("esperaba guard LIVE, got %v", err)
	}
}
