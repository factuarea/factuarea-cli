package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/factuarea/factuarea-cli/internal/config"
)

// Ámbitos de prueba, con la forma que publica la operación de identidad.
const (
	scopeOne = `{"data":{"scope":[` +
		`{"id":"01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8a01","tax_id":"B12345678","name":"Acme Corporation"}` +
		`]}}`
	scopeTwo = `{"data":{"scope":[` +
		`{"id":"01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8a01","tax_id":"B12345678","name":"Acme Corporation"},` +
		`{"id":"01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8a02","tax_id":"B87654321","name":"Acme Logística S.L."}` +
		`]}}`

	scopeFirstID  = "01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8a01"
	scopeSecondID = "01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8a02"
)

// identityServer sirve la operación de identidad con el ámbito dado y cuenta.
func identityServer(t *testing.T, scope string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != identityPath() {
			t.Errorf("petición inesperada (la operación NO debía llegar a la red): %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(scope))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// configFileSnapshot describe el estado del fichero de configuración.
func configFileSnapshot(t *testing.T) string {
	t.Helper()
	file, err := config.ConfigFile()
	if err != nil {
		t.Fatalf("ConfigFile: %v", err)
	}
	info, err := os.Stat(file)
	if os.IsNotExist(err) {
		return "ausente"
	}
	if err != nil {
		t.Fatalf("stat %s: %v", file, err)
	}
	return fmt.Sprintf("%d bytes @ %s", info.Size(), info.ModTime().UTC().Format(time.RFC3339Nano))
}

func companyTestEnv(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", baseURL)
	t.Cleanup(resetCompanyResolution)
	resetCompanyResolution()
}

// TestIdentityPathFollowsTheEmbeddedContract fija que el path de la operación.
func TestIdentityPathFollowsTheEmbeddedContract(t *testing.T) {
	var found *genOp
	for _, op := range generatedOps() {
		if op.OperationID == identityOperationID && len(op.PathParams) == 0 {
			o := op
			found = &o
			break
		}
	}
	if found == nil {
		if identityPath() != identityFallbackPath {
			t.Fatalf("sin operación de identidad embebida, identityPath debe caer en %q; got %q",
				identityFallbackPath, identityPath())
		}
		return
	}
	if want := found.buildPath(nil); identityPath() != want {
		t.Fatalf("identityPath = %q, want %q (el path lo declara el contrato embebido)", identityPath(), want)
	}
}

// TestResolveCompanyPrecedenceChain recorre la cadena congelada de CUATRO.
func TestResolveCompanyPrecedenceChain(t *testing.T) {
	t.Run("el posicional gana y no consulta nada", func(t *testing.T) {
		srv, hits := identityServer(t, scopeOne)
		companyTestEnv(t, srv.URL)

		got, err := resolveCompany(context.Background(), &GlobalFlags{}, frozenCompanyID)
		if err != nil {
			t.Fatalf("resolveCompany: %v", err)
		}
		if got != "" {
			t.Fatalf("con posicional no hay nada que aportar aguas abajo; got %q", got)
		}
		if hits.Load() != 0 {
			t.Fatalf("el posicional no debe consultar el ámbito: %d peticiones", hits.Load())
		}

		op := frozenInvoicesShow()
		pathValues, _, err := op.splitPositionals([]string{frozenCompanyID, frozenInvoiceID}, got)
		if err != nil {
			t.Fatalf("reparto: %v", err)
		}
		if pathValues[0] != frozenCompanyID {
			t.Fatalf("la empresa usada = %q, want el posicional %q", pathValues[0], frozenCompanyID)
		}
	})

	t.Run("la flag gana a la resolución automática", func(t *testing.T) {
		srv, hits := identityServer(t, scopeOne)
		companyTestEnv(t, srv.URL)

		got, err := resolveCompany(context.Background(), &GlobalFlags{Company: "empresa_de_la_flag"}, "")
		if err != nil {
			t.Fatalf("resolveCompany: %v", err)
		}
		if got != "empresa_de_la_flag" {
			t.Fatalf("valor = %q, want el de la flag", got)
		}
		if hits.Load() != 0 {
			t.Fatalf("con flag no hace falta consultar el ámbito: %d peticiones", hits.Load())
		}
	})

	t.Run("con un solo NIF la resolución automática cubre la ausencia", func(t *testing.T) {
		srv, hits := identityServer(t, scopeOne)
		companyTestEnv(t, srv.URL)

		got, err := resolveCompany(context.Background(), &GlobalFlags{}, "")
		if err != nil {
			t.Fatalf("resolveCompany: %v", err)
		}
		if got != scopeFirstID {
			t.Fatalf("valor = %q, want %q (el único NIF del ámbito)", got, scopeFirstID)
		}
		if hits.Load() != 1 {
			t.Fatalf("peticiones de identidad = %d, want 1", hits.Load())
		}
	})

	t.Run("las dos vías explícitas siguen siendo error de uso", func(t *testing.T) {
		srv, _ := identityServer(t, scopeOne)
		companyTestEnv(t, srv.URL)

		g := &GlobalFlags{Company: "otra_empresa"}
		op := frozenInvoicesShow()
		args := []string{frozenCompanyID, frozenInvoiceID}
		company, err := op.resolveCompanyForArgs(context.Background(), g, args)
		if err != nil {
			t.Fatalf("resolveCompanyForArgs: %v", err)
		}
		_, _, err = op.splitPositionals(args, company)
		if err == nil {
			t.Fatal("aportar la empresa por las dos vías debe seguir siendo error de uso")
		}
		if !strings.Contains(err.Error(), "--company") || !strings.Contains(err.Error(), frozenCompanyID) {
			t.Fatalf("el mensaje debe nombrar el conflicto y los dos valores: %v", err)
		}
	})

	t.Run("el entorno y la configuración NO son eslabones de la cadena", func(t *testing.T) {
		srv, _ := identityServer(t, scopeOne)
		companyTestEnv(t, srv.URL)
		// La CLI no declara ninguna variable de entorno de empresa (sólo.
		t.Setenv("FACTUAREA_COMPANY", "empresa_del_entorno")

		got, err := resolveCompany(context.Background(), &GlobalFlags{}, "")
		if err != nil {
			t.Fatalf("resolveCompany: %v", err)
		}
		if got == "empresa_del_entorno" {
			t.Fatal("FACTUAREA_COMPANY no es un eslabón: si alguien lo enlaza, la cadena deja de tener cuatro")
		}
		if got != scopeFirstID {
			t.Fatalf("valor = %q, want %q (el ámbito, no el entorno)", got, scopeFirstID)
		}
	})
}

// TestAmbiguousScopeEnumeratesTheIdentifiersWithoutCallingTheOperation es el
// eslabón 4: con varias empresas alcanzables, enumera y no elige.
func TestAmbiguousScopeEnumeratesTheIdentifiersWithoutCallingTheOperation(t *testing.T) {
	srv, hits := identityServer(t, scopeTwo)
	companyTestEnv(t, srv.URL)

	_, err := runFrozenOp(t, srv.URL, frozenInvoicesShow(), &GlobalFlags{}, frozenInvoiceID)
	if err == nil {
		t.Fatal("con dos NIF en el ámbito y sin empresa por ninguna vía debe fallar")
	}
	for _, want := range []string{
		scopeFirstID, "B12345678", "Acme Corporation",
		scopeSecondID, "B87654321", "Acme Logística S.L.",
		"--company",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("el mensaje debe enumerar %q; got:\n%v", want, err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("peticiones de identidad = %d, want 1", hits.Load())
	}
}

// TestResolvedCompanyIsCachedPerProcessAndNeverWritten fija las dos mitades.
func TestResolvedCompanyIsCachedPerProcessAndNeverWritten(t *testing.T) {
	// El fichero de configuración NO se redirige a un temporal.
	before := configFileSnapshot(t)

	srv, hits := identityServer(t, scopeOne)
	companyTestEnv(t, srv.URL)

	for i := range 3 {
		got, err := resolveCompany(context.Background(), &GlobalFlags{}, "")
		if err != nil {
			t.Fatalf("resolución %d: %v", i+1, err)
		}
		if got != scopeFirstID {
			t.Fatalf("resolución %d = %q, want %q", i+1, got, scopeFirstID)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("peticiones de identidad = %d, want 1: el valor se memoiza por proceso", hits.Load())
	}

	if after := configFileSnapshot(t); after != before {
		t.Fatalf("la resolución NO debe escribir en disco: la configuración pasó de %q a %q", before, after)
	}

	// Otra credencial es otro ámbito: la memoria va indexada por la credencial.
	t.Setenv("FACTUAREA_API_KEY", "fact_test_bbbbbbbbbbbbbbbbbbbbbbbb")
	if _, err := resolveCompany(context.Background(), &GlobalFlags{}, ""); err != nil {
		t.Fatalf("con otra credencial: %v", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("peticiones de identidad = %d, want 2: la memoria no se comparte entre credenciales", hits.Load())
	}
}
