package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

func TestAPICommandGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account" {
			t.Errorf("bad path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"acct_1","object":"account"}`))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"api", "get", "/v1/account", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), `"id":"acct_1"`) {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestAPICommandLiveGuardBlocksMutation(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "fact_live_bbbbbbbbbbbbbbbbbbbbbbbb")
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"api", "post", "/v1/invoices", "-d", "{}"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "LIVE") {
		t.Fatalf("expected live guard error, got %v", err)
	}
	if got := exit.ForError(err); got != exit.Usage {
		t.Fatalf("expected exit.Usage (%d) for live guard, got %d", exit.Usage, got)
	}
}
