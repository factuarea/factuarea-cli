package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// frozenInvoicesCreate: `POST /companies/{company}/invoices`, operación de eje
// con cuerpo TIPADO, que es la única forma en la que `--skeleton` se registra.
func frozenInvoicesCreate() genOp {
	return genOp{
		OperationID: "public-api.v1.invoices.store", Method: "POST",
		Path:   "/companies/{company}/invoices",
		Action: "create", Summary: "Create an invoice",
		RequiredScope: "invoices:write",
		Groups:        []string{"invoices"},
		PathParams: []genParam{
			{Name: "company", In: "path", Type: "string", Required: true},
		},
		QueryParams: []genParam{},
		Body: &genBody{
			Kind: "json",
			Fields: []genBodyField{
				{Name: "client_id", Type: "string", Required: true},
				{Name: "issue_date", Type: "string", Required: true},
				{Name: "notes", Type: "string"},
			},
		},
	}
}

// TestSkeletonNeitherResolvesTheCompanyNorTouchesTheNetwork fija la promesa de
// la ayuda de la flag: imprime la plantilla SIN llamar a la API.
func TestSkeletonNeitherResolvesTheCompanyNorTouchesTheNetwork(t *testing.T) {
	srv := silentServer(t)

	t.Run("sin empresa por ninguna vía", func(t *testing.T) {
		out, err := runFrozenOp(t, srv.URL, frozenInvoicesCreate(), &GlobalFlags{}, "--skeleton")
		if err != nil {
			t.Fatalf("--skeleton no debe fallar por falta de empresa: %v", err)
		}
		for _, field := range []string{`"client_id"`, `"issue_date"`, `"notes"`} {
			if !strings.Contains(out, field) {
				t.Fatalf("la plantilla debe declarar %s; salida: %s", field, out)
			}
		}
	})

	t.Run("la plantilla es la misma con empresa que sin ella", func(t *testing.T) {
		sin, err := runFrozenOp(t, srv.URL, frozenInvoicesCreate(), &GlobalFlags{}, "--skeleton")
		if err != nil {
			t.Fatalf("sin empresa: %v", err)
		}
		con, err := runFrozenOp(t, srv.URL, frozenInvoicesCreate(), &GlobalFlags{Company: frozenCompanyID}, "--skeleton")
		if err != nil {
			t.Fatalf("con empresa: %v", err)
		}
		if sin != con {
			t.Fatalf("la plantilla depende del contrato, no de la invocación:\n  sin: %s\n  con: %s", sin, con)
		}
	})

	t.Run("sin --skeleton la falta de empresa sigue siendo un error accionable", func(t *testing.T) {
		// El corte NO puede haberse llevado por delante la cadena.
		_, err := runFrozenOp(t, "", frozenInvoicesCreate(), &GlobalFlags{}, "-d", "{}")
		if err == nil {
			t.Fatal("sin empresa y sin --skeleton la operación debe fallar")
		}
		if !strings.Contains(err.Error(), "--company") {
			t.Fatalf("el fallo debe nombrar --company: %v", err)
		}
	})
}

// TestDryRunNeitherResolvesTheCompanyNorTouchesTheNetwork es la misma promesa
// para la flag hermana, que además fallaba con un ámbito de dos NIF.
func TestDryRunNeitherResolvesTheCompanyNorTouchesTheNetwork(t *testing.T) {
	srv := silentServer(t)

	out, err := runFrozenOp(t, srv.URL, frozenInvoicesCreate(), &GlobalFlags{},
		"--dry-run", "-d", `{"client_id":"cli_1","issue_date":"2026-01-02"}`)
	if err != nil {
		t.Fatalf("--dry-run no debe fallar por falta de empresa: %v", err)
	}
	for _, field := range []string{`"client_id"`, `"issue_date"`} {
		if !strings.Contains(out, field) {
			t.Fatalf("el cuerpo compilado debe llevar %s; salida: %s", field, out)
		}
	}
}

// TestSkeletonStillRunsTheValidationsBeforeItPrints fija que el corte que cumple
// la promesa de «sin red» no se lleva por delante ninguna validación.
func TestSkeletonStillRunsTheValidationsBeforeItPrints(t *testing.T) {
	srv := silentServer(t)

	t.Run("falta un filtro requerido", func(t *testing.T) {
		op := frozenInvoicesCreate()
		op.QueryParams = []genParam{{Name: "series", In: "query", Type: "string", Required: true}}
		_, err := runFrozenOp(t, srv.URL, op, &GlobalFlags{}, "--skeleton")
		if err == nil {
			t.Fatal("con un filtro requerido ausente, --skeleton debe fallar")
		}
		if !strings.Contains(err.Error(), "series") {
			t.Fatalf("el fallo debe nombrar el filtro: %v", err)
		}
	})

	t.Run("dos fuentes del cuerpo a la vez", func(t *testing.T) {
		_, err := runFrozenOp(t, srv.URL, frozenInvoicesCreate(), &GlobalFlags{},
			"--skeleton", "-d", "{}", "--data-file", "cuerpo.json")
		if err == nil {
			t.Fatal("--data y --data-file a la vez deben seguir siendo un error de uso con --skeleton")
		}
		if !strings.Contains(err.Error(), "--data-file") {
			t.Fatalf("el fallo debe nombrar las dos fuentes: %v", err)
		}
	})
}

// TestRangeArgsErrorIsTranslated cubre el tercer mensaje de aridad de cobra,
// `RangeArgs`, que es el de toda operación con un posicional opcional.
func TestRangeArgsErrorIsTranslated(t *testing.T) {
	got := translateCobraError("accepts between 0 and 1 arg(s), received 2")
	want := "este comando acepta entre 0 y 1 argumento(s), recibió 2"
	if got != want {
		t.Fatalf("traducción = %q, want %q", got, want)
	}

	// Las otras dos no se han movido de sitio.
	if got := translateCobraError("accepts 2 arg(s), received 3"); got != "este comando acepta 2 argumento(s), recibió 3" {
		t.Fatalf("ExactArgs = %q", got)
	}
	if got := translateCobraError("accepts at most 1 arg(s), received 2"); got != "este comando acepta como máximo 1 argumento(s), recibió 2" {
		t.Fatalf("MaximumNArgs = %q", got)
	}
}

// TestRangeArgsErrorTextMatchesCobra ata la expresión regular al literal que
// cobra produce HOY, en vez de a uno copiado a mano.
func TestRangeArgsErrorTextMatchesCobra(t *testing.T) {
	err := cobra.RangeArgs(0, 1)(&cobra.Command{Use: "x"}, []string{"a", "b"})
	if err == nil {
		t.Fatal("dos argumentos para un rango 0..1 deben fallar")
	}
	if !reAcceptsBetween.MatchString(err.Error()) {
		t.Fatalf("la expresión regular ya no casa con el literal de cobra: %q", err.Error())
	}
	if translated := translateCobraError(err.Error()); translated == err.Error() {
		t.Fatalf("el mensaje de cobra sigue saliendo sin traducir: %q", translated)
	}
}
