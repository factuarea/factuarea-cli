package cmd

import (
	"bytes"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

func execArgs(t *testing.T, args ...string) error {
	t.Helper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	return root.Execute()
}

func TestUnknownSubcommandIsUsageError(t *testing.T) {
	err := execArgs(t, "invoices", "get", "x")
	if err == nil {
		t.Fatal("subcomando inexistente debe fallar, no imprimir ayuda con exit 0")
	}
	if code := exit.ForError(err); code != exit.Usage {
		t.Fatalf("exit code = %d, want %d (Usage)", code, exit.Usage)
	}
}

func TestUnknownTopLevelIsUsageError(t *testing.T) {
	err := execArgs(t, "bogus")
	if err == nil || exit.ForError(err) != exit.Usage {
		t.Fatalf("top-level inexistente debe dar exit Usage; got err=%v code=%d", err, exit.ForError(err))
	}
}

func TestGroupAloneShowsHelpNoError(t *testing.T) {
	if err := execArgs(t, "invoices"); err != nil {
		t.Fatalf("grupo solo debe mostrar ayuda sin error; got %v", err)
	}
}
