package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

func runCmd(t *testing.T, baseURL string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	if baseURL != "" {
		t.Setenv("FACTUAREA_BASE_URL", baseURL)
	}
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestTypedFlagsBuildBodyWithTypes(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"cli_1"}}`))
	}))
	t.Cleanup(srv.Close)

	_, err := runCmd(t, srv.URL, "clients", "create", "--skip-scope-check",
		"--name", "ACME SL", "--tax-id", "B12345678",
		"--payment-terms-days", "30", "--address.city", "Madrid",
		"--billing-emails", "a@x.com,b@x.com", "--metadata", "erp=CLI-1", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got["name"] != "ACME SL" || got["tax_id"] != "B12345678" {
		t.Errorf("strings mal mapeados: %v", got)
	}
	if n, ok := got["payment_terms_days"].(float64); !ok || n != 30 {
		t.Errorf("payment_terms_days debe ser número 30: %v", got["payment_terms_days"])
	}
	addr, ok := got["address"].(map[string]any)
	if !ok || addr["city"] != "Madrid" {
		t.Errorf("address.city debe agruparse en objeto: %v", got["address"])
	}
	emails, ok := got["billing_emails"].([]any)
	if !ok || len(emails) != 2 {
		t.Errorf("billing_emails debe ser slice de 2: %v", got["billing_emails"])
	}
	meta, ok := got["metadata"].(map[string]any)
	if !ok || meta["erp"] != "CLI-1" {
		t.Errorf("metadata debe ser map: %v", got["metadata"])
	}
}

func TestTypedFlagsZeroVsOmitted(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"cli_1"}}`))
	}))
	t.Cleanup(srv.Close)

	_, err := runCmd(t, srv.URL, "clients", "create", "--skip-scope-check", "--name", "X", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, present := got["default_discount"]; present {
		t.Errorf("default_discount omitido no debe estar presente: %v", got)
	}

	got = nil
	_, err = runCmd(t, srv.URL, "clients", "create", "--skip-scope-check", "--name", "X", "--default-discount", "0", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if v, ok := got["default_discount"].(float64); !ok || v != 0 {
		t.Errorf("--default-discount 0 debe enviarse como 0: %v", got["default_discount"])
	}
}

func TestMixingFlagsAndRawDataRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("la red NO debe tocarse al mezclar flags con -d")
	}))
	t.Cleanup(srv.Close)

	_, err := runCmd(t, srv.URL, "clients", "create", "--name", "X", "-d", `{"name":"Y"}`)
	if err == nil || !strings.Contains(err.Error(), "no mezcles") {
		t.Fatalf("esperaba error de uso por mezcla, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit code = %d, want Usage", exit.ForError(err))
	}
}

func TestDataFromStdin(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
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
	root.SetIn(strings.NewReader(`{"name":"PIPED"}`))
	root.SetArgs([]string{"clients", "create", "--skip-scope-check", "-d", "-", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got["name"] != "PIPED" {
		t.Errorf("stdin no llegó al body: %v", got)
	}
}

func TestDryRunPrintsBodyNoNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("la red NO debe tocarse en --dry-run")
	}))
	t.Cleanup(srv.Close)

	out, err := runCmd(t, srv.URL, "clients", "create", "--name", "ACME", "--dry-run")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var body map[string]any
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(out)), &body); jerr != nil {
		t.Fatalf("dry-run debe imprimir JSON válido: %q (%v)", out, jerr)
	}
	if body["name"] != "ACME" {
		t.Errorf("dry-run body mal: %v", body)
	}
}

func TestSkeletonEmitsTemplateNoNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("la red NO debe tocarse en --skeleton")
	}))
	t.Cleanup(srv.Close)

	out, err := runCmd(t, srv.URL, "clients", "create", "--skeleton")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var body map[string]any
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(out)), &body); jerr != nil {
		t.Fatalf("skeleton debe ser JSON válido: %q (%v)", out, jerr)
	}
	if _, ok := body["name"]; !ok {
		t.Errorf("skeleton debe incluir el campo required name: %v", body)
	}
	if pm, ok := body["payment_method"].(string); !ok || !strings.Contains(pm, "direct_debit") {
		t.Errorf("skeleton debe incluir hints de enum: %v", body["payment_method"])
	}
	if addr, ok := body["address"].(map[string]any); !ok || addr["city"] == nil {
		t.Errorf("skeleton debe anidar objetos prof.1: %v", body["address"])
	}
}

func TestObjectArrayOpHasNoLineFlagsAndDirectsToFile(t *testing.T) {
	_, err := runCmd(t, "", "invoices", "create", "--lines", "x", "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "flag desconocido") {
		t.Fatalf("invoices create NO debe registrar --lines, got %v", err)
	}

	out, herr := runCmd(t, "", "invoices", "create", "--help")
	if herr != nil {
		t.Fatalf("help: %v", herr)
	}
	if !strings.Contains(out, "--data-file") || !strings.Contains(out, "lista de objetos") {
		t.Errorf("help debe dirigir a --data-file para arrays de objetos: %s", out)
	}
}

func TestUpdateHelpDescribesPartialEdit(t *testing.T) {
	out, err := runCmd(t, "", "clients", "update", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(out, "Edición parcial") || !strings.Contains(out, "los omitidos se conservan") {
		t.Errorf("update help debe describir la edición parcial: %s", out)
	}
}

func TestManifestIncludesFieldSchema(t *testing.T) {
	out, err := runCmd(t, "", "commands", "--json")
	if err != nil {
		t.Fatalf("commands: %v", err)
	}
	var manifest []map[string]any
	if jerr := json.Unmarshal([]byte(out), &manifest); jerr != nil {
		t.Fatalf("manifest no es JSON: %v", jerr)
	}
	var create map[string]any
	for _, e := range manifest {
		if e["command"] == "factuarea clients create" {
			create = e
			break
		}
	}
	if create == nil {
		t.Fatal("manifest sin clients create")
	}
	fields, ok := create["body_fields"].([]any)
	if !ok || len(fields) == 0 {
		t.Fatalf("clients create debe traer body_fields: %v", create["body_fields"])
	}
	var sawName, sawEnum bool
	for _, raw := range fields {
		f := raw.(map[string]any)
		if f["name"] == "name" && f["required"] == true {
			sawName = true
		}
		if f["name"] == "payment-method" {
			if enum, ok := f["enum"].([]any); ok && len(enum) > 0 {
				sawEnum = true
			}
		}
	}
	if !sawName {
		t.Error("manifest debe marcar name como required")
	}
	if !sawEnum {
		t.Error("manifest debe incluir enum de payment_method")
	}
}
