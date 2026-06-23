package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

func runRoot(t *testing.T, in string, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if in != "" {
		root.SetIn(strings.NewReader(in))
	}
	root.SetArgs(args)
	return out.String(), root.Execute()
}

func TestBaseURLRejectsInsecureNonLoopback(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", "http://api.example.com")
	_, err := runRoot(t, "", "whoami", "--json")
	if err == nil || !strings.Contains(err.Error(), "http://") {
		t.Fatalf("esperaba rechazo de http no-loopback, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
	}
}

func TestBaseURLAllowsInsecureWithFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"acct_1"}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	if _, err := runRoot(t, "", "whoami", "--allow-insecure-transport", "--json"); err != nil {
		t.Fatalf("loopback http debe pasar incluso con el flag: %v", err)
	}
}

func TestBaseURLLoopbackHTTPAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"acct_1"}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	if _, err := runRoot(t, "", "whoami", "--json"); err != nil {
		t.Fatalf("loopback http debe permitirse sin flag: %v", err)
	}
}

func TestLoginAPIKeyRejectsLiteralWithoutEcho(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "")
	secret := "fact_live_SUPERSECRETPRODVALUE"
	out, err := runRoot(t, "", "login", "--api-key="+secret)
	if err == nil {
		t.Fatal("esperaba rechazo del valor literal")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(out, secret) {
		t.Fatalf("el secreto NO debe aparecer en el error/salida: err=%v out=%s", err, out)
	}
	if strings.Contains(err.Error(), "ParseBool") {
		t.Fatalf("no debe filtrar internals de Go: %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
	}
}

func TestReadKeyPrefersEnv(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	cmd := NewRootCmd()
	cmd.SetIn(strings.NewReader(""))
	key, err := readKey(cmd, false, true)
	if err != nil {
		t.Fatalf("readKey: %v", err)
	}
	if key != "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("readKey debe devolver la env, got %q", key)
	}
}

func TestReadKeyNoInputWithoutKeyFailsFast(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "")
	cmd := NewRootCmd()
	cmd.SetIn(strings.NewReader(""))
	_, err := readKey(cmd, false, true)
	if err == nil {
		t.Fatal("--no-input sin key ni stdin debe fallar, no colgar")
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
	}
}

func TestListenRejectsNonPositivePollInterval(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	_, err := runRoot(t, "", "listen", "--forward-to", "http://localhost:9099", "--poll-interval", "0s")
	if err == nil || !strings.Contains(err.Error(), "poll-interval") {
		t.Fatalf("esperaba error de validación de poll-interval, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
	}
}

func TestInvalidJSONBodyRejectedClientSide(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("la red NO debe tocarse con JSON inválido: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	_, err := runRoot(t, "", "clients", "create", "--skip-scope-check", "-d", "{not json}")
	if err == nil || !strings.Contains(err.Error(), "JSON inválido") {
		t.Fatalf("esperaba error de JSON inválido, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
	}
}

func TestNonObjectJSONBodyRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("la red NO debe tocarse: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	_, err := runRoot(t, "", "clients", "create", "--skip-scope-check", "-d", `["a","b"]`)
	if err == nil || !strings.Contains(err.Error(), "objeto JSON") {
		t.Fatalf("esperaba rechazo de array, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
	}
}

func TestInvalidJSONRejectedInDryRun(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	_, err := runRoot(t, "", "clients", "create", "-d", `{"a":1} trailing`, "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "JSON inválido") {
		t.Fatalf("--dry-run debe validar el JSON, got %v", err)
	}
}

func TestEmptyResourceIDRejectedClientSide(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("la red NO debe tocarse con id vacío: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	_, err := runRoot(t, "", "invoices", "show", "   ")
	if err == nil || !strings.Contains(err.Error(), "id del recurso") {
		t.Fatalf("esperaba rechazo de id vacío, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
	}
}

func TestAPIPrefixesV1(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"acct_1"}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	if _, err := runRoot(t, "", "api", "get", "/account", "--json"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotPath != "/v1/account" {
		t.Fatalf("api debe prefijar /v1, got %s", gotPath)
	}
}

func TestAPIDoesNotDoublePrefixV1(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	if _, err := runRoot(t, "", "api", "get", "/v1/account", "--json"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotPath != "/v1/account" {
		t.Fatalf("api no debe duplicar /v1, got %s", gotPath)
	}
}

func TestAPIRejectsInvalidMethod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("método inválido no debe tocar la red: %s", r.Method)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	_, err := runRoot(t, "", "api", "fetch", "/account")
	if err == nil || !strings.Contains(err.Error(), "método HTTP no válido") {
		t.Fatalf("esperaba rechazo de método inválido, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
	}
}

func TestQuotesPDFWritesToOutputFile(t *testing.T) {
	pdf := []byte("%PDF-1.7\nfake")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(pdf)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	dst := filepath.Join(t.TempDir(), "quote.pdf")
	out, err := runRoot(t, "", "quotes", "pdf", "qt_1", "-o", dst)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(out, "%PDF") {
		t.Fatalf("el binario NO debe volcarse a stdout: %s", out)
	}
	got, rerr := os.ReadFile(dst)
	if rerr != nil {
		t.Fatalf("leer %s: %v", dst, rerr)
	}
	if !bytes.Equal(got, pdf) {
		t.Fatalf("el fichero -o debe contener el PDF, got %q", got)
	}
}

func TestSeriesDefaultExposesDocumentType(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
			return
		}
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"ser_1"}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	if _, err := runRoot(t, "", "series", "default", "--document_type", "invoice", "--json"); err != nil {
		t.Fatalf("series default debe ser invocable: %v", err)
	}
	if !strings.Contains(gotQuery, "document_type=invoice") {
		t.Fatalf("document_type debe viajar como query, got %q", gotQuery)
	}
}
