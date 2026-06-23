package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestVersionCommandPrintsJSON(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"version", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &payload); err != nil {
		t.Fatalf("version --json debe ser JSON válido: %q (%v)", out.String(), err)
	}
	if payload["version"] == "" || payload["spec"] == "" {
		t.Fatalf("version --json debe incluir version y spec: %v", payload)
	}
}
