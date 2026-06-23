package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

func TestGeneratedListCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
			return
		}
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

func TestGeneratedIrreversibleRequiresConfirmInNoInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("la red NO debe tocarse: recibí %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"invoices", "delete", "inv_123", "--skip-scope-check", "--no-input"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("esperaba fallo pidiendo --confirm, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit code = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
	}
}

func TestGeneratedIrreversibleConfirmFlagReachesNetwork(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" && r.URL.Path == "/v1/invoices/inv_123" {
			hit = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"inv_123","deleted":true}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"invoices", "delete", "inv_123", "--skip-scope-check", "--confirm", "inv_123", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !hit {
		t.Fatal("la red debió recibir el DELETE tras --confirm correcto")
	}
}

func TestGeneratedMissingScopeBlocksWithExit4(t *testing.T) {
	var deleteHit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/account":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["clients:read"]}}}`))
		case r.URL.Path == "/v1/invoices/inv_123":
			deleteHit = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("petición inesperada: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"invoices", "delete", "inv_123", "--confirm", "inv_123", "--json"})
	err := root.Execute()
	if err == nil {
		t.Fatal("esperaba bloqueo por scope insuficiente")
	}
	if exit.ForError(err) != exit.Perm {
		t.Fatalf("exit code = %d, want %d (Perm)", exit.ForError(err), exit.Perm)
	}
	if deleteHit {
		t.Fatal("el endpoint de la operación NO debió recibir la petición (bloqueado pre-red)")
	}
}

func TestGeneratedSkipScopeCheckBypassesBlock(t *testing.T) {
	var deleteHit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/v1/account" {
			t.Fatal("con --skip-scope-check no debe llamarse a /v1/account")
		}
		if r.URL.Path == "/v1/invoices/inv_123" {
			deleteHit = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"inv_123","deleted":true}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"invoices", "delete", "inv_123", "--confirm", "inv_123", "--skip-scope-check", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !deleteHit {
		t.Fatal("la red debió recibir el DELETE con --skip-scope-check")
	}
}
