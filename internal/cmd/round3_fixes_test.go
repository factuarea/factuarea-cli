package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
	"github.com/factuarea/factuarea-cli/internal/output"
)

func TestMultipartMissingFileIsUsageError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("la red NO debe tocarse sin --file-image: %s", r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	_, err := runCmd(t, srv.URL, "products", "gallery", "upload", "prod_1", "--skip-scope-check")
	if err == nil || !strings.Contains(err.Error(), "--file-<campo>") {
		t.Fatalf("esperaba error de uso por falta de --file-image, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want Usage (2)", exit.ForError(err))
	}
}

func TestIsTTYDevNullIsNotTerminal(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("abrir %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if output.IsTTY(f) {
		t.Fatalf("%s es un char device pero NO un TTY real; IsTTY debe ser false", os.DevNull)
	}
}

func TestIsTTYRegularFileIsNotTerminal(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tty")
	if err != nil {
		t.Fatalf("crear temp: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if output.IsTTY(f) {
		t.Fatalf("un fichero regular NO es un TTY; IsTTY debe ser false")
	}
}

func TestLoginAcceptsDashPositionalAsStdin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	os.Unsetenv("FACTUAREA_API_KEY")
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa\n"))
	root.SetArgs([]string{"login", "-", "--quiet"})
	if err := root.Execute(); err != nil {
		t.Fatalf("login - (posicional) debe leer la key de stdin: %v", err)
	}
}

func TestLoginRejectsLiteralKeyPositional(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("la red NO debe tocarse con una key literal posicional")
	}))
	t.Cleanup(srv.Close)

	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	os.Unsetenv("FACTUAREA_API_KEY")
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"login", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "argumento posicional") {
		t.Fatalf("una key literal posicional debe rechazarse, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want Usage (2)", exit.ForError(err))
	}
}
