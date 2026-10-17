package gen

import (
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/spec"
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

// specWithNonCanonicalOperation es un contrato mínimo con DOS operaciones: una
// con el prefijo canónico y otra sin él. Alimenta al generador por el único
// camino que tiene (`spec.Raw`, el documento embebido) para comprobar que la no
// canónica NO se genera y SÍ se devuelve en la lista de no conformes, que es lo
// que `internal/gen/main.go` convierte en salida distinta de cero.
const specWithNonCanonicalOperation = `{
  "openapi": "3.1.0",
  "info": {"title": "contrato de prueba", "version": "1.0.0"},
  "paths": {
    "/widgets": {
      "get": {
        "operationId": "public-api.v1.widgets.list",
        "summary": "Lista widgets",
        "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "object"}}}}}
      }
    },
    "/widgets/purge": {
      "post": {
        "operationId": "widgets.purge",
        "summary": "Operación sin el prefijo canónico",
        "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "object"}}}}}
      }
    }
  }
}`

func TestGenerateReportsNonCanonicalOperationIDs(t *testing.T) {
	original := spec.Raw
	t.Cleanup(func() { spec.Raw = original })
	spec.Raw = []byte(specWithNonCanonicalOperation)

	out, nonConforming, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(nonConforming) != 1 || nonConforming[0] != "widgets.purge" {
		t.Fatalf("nonConforming = %v, want [widgets.purge]", nonConforming)
	}
	s := string(out)
	if !strings.Contains(s, `OperationID: "public-api.v1.widgets.list"`) {
		t.Fatal("la operación canónica debe seguir generándose")
	}
	if strings.Contains(s, "widgets.purge") {
		t.Fatal("la operación no canónica NO debe aparecer en la tabla generada")
	}
}

func TestGenerateReportsEveryNonCanonicalOperationID(t *testing.T) {
	original := spec.Raw
	t.Cleanup(func() { spec.Raw = original })
	spec.Raw = []byte(strings.ReplaceAll(
		strings.ReplaceAll(specWithNonCanonicalOperation, `"public-api.v1.widgets.list"`, `"widgets.list"`),
		`"widgets.purge"`, `"public-api.v1"`,
	))

	_, nonConforming, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	// El mensaje del generador ENUMERA a los culpables: si sólo devolviera el
	// primero, el arreglo en el backend se haría de uno en uno a ciegas.
	want := []string{"public-api.v1", "widgets.list"} // ordenados por `spec.Load`
	if len(nonConforming) != len(want) {
		t.Fatalf("nonConforming = %v, want %v", nonConforming, want)
	}
	for i := range want {
		if nonConforming[i] != want[i] {
			t.Fatalf("nonConforming = %v, want %v", nonConforming, want)
		}
	}
}
