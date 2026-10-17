package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCommandsManifestJSON(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"commands", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var manifest []map[string]any
	if err := json.Unmarshal(out.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest no es JSON: %v", err)
	}
	if len(manifest) < 200 {
		t.Fatalf("esperaba >=200 comandos, got %d", len(manifest))
	}

	var del map[string]any
	for _, e := range manifest {
		if e["command"] == "factuarea invoices delete" {
			del = e
			break
		}
	}
	if del == nil {
		t.Fatal("manifest sin la entrada `factuarea invoices delete`")
	}
	if irr, ok := del["irreversible"].(bool); !ok || !irr {
		t.Errorf("invoices delete debe traer irreversible=true en el manifiesto, got %v", del["irreversible"])
	}
	if scope, _ := del["required_scope"].(string); scope != "invoices:delete" {
		t.Errorf("invoices delete required_scope = %q, want invoices:delete", scope)
	}
}

// manifestEntryFor devuelve la entrada del manifiesto de comandos del comando dado.
func manifestEntryFor(t *testing.T, command string) map[string]any {
	t.Helper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"commands", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var manifest []map[string]any
	if err := json.Unmarshal(out.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest no es JSON: %v", err)
	}
	for _, e := range manifest {
		if e["command"] == command {
			return e
		}
	}
	t.Fatalf("el manifiesto no trae la entrada %q", command)
	return nil
}

// TestCommandsManifestPublishesTheAccountSlot: una lectura del eje de cuenta
// publica `account` en `optional_args`; un comando de `managed-accounts` (la
// cuenta gestionada es un recurso) y una irreversible con el slot final no.
func TestCommandsManifestPublishesTheAccountSlot(t *testing.T) {
	read := manifestEntryFor(t, "factuarea account members list")
	optional, _ := read["optional_args"].(map[string]any)
	if flag, _ := optional["account"].(string); flag != "--account" {
		t.Errorf("account members list: optional_args = %v, want {account: \"--account\"}", read["optional_args"])
	}
	if args, _ := read["args"].([]any); len(args) != 1 || args[0] != "account" {
		t.Errorf("account members list: args = %v; el orden y los nombres de los posicionales no cambian", read["args"])
	}

	for _, command := range []string{
		"factuarea managed-accounts show",
		"factuarea managed-accounts management end",
		"factuarea account owner transfer",
	} {
		if optional, present := manifestEntryFor(t, command)["optional_args"]; present {
			t.Errorf("%s no debe publicar optional_args (la cuenta no es rellenable): %v", command, optional)
		}
	}
}

// TestAccountFlagIsAGlobalOption: `--account` es una opción global de la CLI.
func TestAccountFlagIsAGlobalOption(t *testing.T) {
	root := NewRootCmd()
	global := root.PersistentFlags().Lookup("account")
	if global == nil {
		t.Fatal("la raíz no declara el flag persistente --account")
	}
	if global.Usage == "" {
		t.Error("--account no documenta para qué sirve")
	}
	for _, want := range []string{"{account}", "lecturas", "managed-accounts"} {
		if !strings.Contains(global.Usage, want) {
			t.Errorf("la descripción de --account debe mencionar %q; got %q", want, global.Usage)
		}
	}
	c, rest, err := root.Find([]string{"account", "members", "list"})
	if err != nil || len(rest) > 0 {
		t.Fatalf("no encuentro `account members list`: %v %v", rest, err)
	}
	if c.InheritedFlags().Lookup("account") == nil {
		t.Error("account members list no hereda --account: no sería una opción global")
	}
}
