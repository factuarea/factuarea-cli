package gen

import (
	"strings"
	"testing"
)

func TestGenerateProducesCompilableTable(t *testing.T) {
	out, _, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "func generatedOps() []genOp") {
		t.Fatal("falta generatedOps()")
	}
	if !strings.Contains(s, `OperationID: "public-api.v1.invoices.create"`) {
		t.Fatal("falta invoices.create en la tabla generada")
	}
	if strings.Count(s, "OperationID:") < 200 {
		t.Fatalf("esperaba >=200 ops, got %d", strings.Count(s, "OperationID:"))
	}
	if !strings.Contains(s, "Fields: []genBodyField{") {
		t.Fatal("la tabla generada debe incluir Fields del body")
	}
	if !strings.Contains(s, `Kind: "scalar"`) || !strings.Contains(s, `Kind: "object_array"`) {
		t.Fatal("la tabla generada debe clasificar campos (scalar / object_array)")
	}
	if !strings.Contains(s, "HasObjectArray: true") {
		t.Fatal("la tabla generada debe marcar HasObjectArray para ops con arrays de objetos")
	}
}
