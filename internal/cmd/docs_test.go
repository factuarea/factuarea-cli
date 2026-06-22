package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func runDocsSearch(t *testing.T, args ...string) string {
	t.Helper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"docs", "search"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("docs search %v devolvió error: %v", args, err)
	}
	return out.String()
}

func TestDocsSearchFindsInvoices(t *testing.T) {
	var hits []map[string]any
	if err := json.Unmarshal([]byte(runDocsSearch(t, "invoice", "--json")), &hits); err != nil {
		t.Fatalf("salida no es JSON: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("esperaba al menos una coincidencia para \"invoice\"")
	}
	found := false
	for _, h := range hits {
		if cmd, ok := h["command"].(string); ok && strings.Contains(cmd, "invoices") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("esperaba alguna entrada con \"invoices\" en command")
	}
}

func TestDocsSearchJSONShape(t *testing.T) {
	var hits []map[string]any
	if err := json.Unmarshal([]byte(runDocsSearch(t, "invoice", "--json")), &hits); err != nil {
		t.Fatalf("salida no es JSON: %v", err)
	}
	for i, h := range hits {
		for _, key := range []string{"command", "method", "path"} {
			if v, ok := h[key].(string); !ok || v == "" {
				t.Fatalf("entrada %d sin %q válido: %#v", i, key, h)
			}
		}
	}
}

func TestDocsSearchNoResults(t *testing.T) {
	var hits []map[string]any
	if err := json.Unmarshal([]byte(runDocsSearch(t, "zzzznotarealthing", "--json")), &hits); err != nil {
		t.Fatalf("salida no es JSON: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("esperaba 0 coincidencias, got %d", len(hits))
	}
}
