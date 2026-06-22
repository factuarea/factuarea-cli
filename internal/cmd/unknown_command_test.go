package cmd

import (
	"bytes"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

// execArgs ejecuta el CLI con args contra un root nuevo, capturando la salida,
// y devuelve el error de Execute (para inspeccionar el exit code derivado).
func execArgs(t *testing.T, args ...string) error {
	t.Helper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	return root.Execute()
}

// Un subcomando inexistente bajo un grupo generado (p.ej. `invoices get`, cuando
// la acción real es `show`) NO debe imprimir ayuda y salir con exit 0 (un agente
// lo interpretaría como éxito). Debe fallar como uso incorrecto (exit 2).
func TestUnknownSubcommandIsUsageError(t *testing.T) {
	err := execArgs(t, "invoices", "get", "x")
	if err == nil {
		t.Fatal("subcomando inexistente debe fallar, no imprimir ayuda con exit 0")
	}
	if code := exit.ForError(err); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (Usage)", code, exit.Usage)
	}
}

// Un comando top-level inexistente también es uso incorrecto (exit 2), no exit 1.
func TestUnknownTopLevelIsUsageError(t *testing.T) {
	err := execArgs(t, "bogus")
	if err == nil || exit.ForError(err) != exit.Usage {
		t.Fatalf("top-level inexistente debe dar exit Usage; got err=%v code=%d", err, exit.ForError(err))
	}
}

// Un grupo invocado sin acción muestra su ayuda sin error (exit 0).
func TestGroupAloneShowsHelpNoError(t *testing.T) {
	if err := execArgs(t, "invoices"); err != nil {
		t.Fatalf("grupo solo debe mostrar ayuda sin error; got %v", err)
	}
}
