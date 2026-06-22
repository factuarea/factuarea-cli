package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

func TestTriggerRejectsLiveKey(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "fact_live_bbbbbbbbbbbbbbbbbbbbbbbb")

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"trigger", "invoice.paid"})
	err := root.Execute()
	if err == nil {
		t.Fatal("una key fact_live_ debe ser rechazada por el guard sandbox")
	}
	if got := exit.ForError(err); got != exit.Usage {
		t.Fatalf("expected exit.Usage (%d) for sandbox guard, got %d", exit.Usage, got)
	}
}

func TestTriggerListShowsSupported(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"trigger", "--list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "invoice.paid") {
		t.Fatalf("--list debe imprimir los eventos soportados; got %q", out.String())
	}
}

func TestTriggerClientCreatedHitsAPI(t *testing.T) {
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/clients") {
			posted = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"cli_1"}}`))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"trigger", "client.created"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !posted {
		t.Fatal("trigger client.created debe hacer POST /v1/clients")
	}
}
