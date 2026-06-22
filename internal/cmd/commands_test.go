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
}
