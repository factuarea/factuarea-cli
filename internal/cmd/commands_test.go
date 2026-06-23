package cmd

import (
	"bytes"
	"encoding/json"
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
