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

// opBlock devuelve el texto generado de una operación: desde su OperationID hasta
// el de la siguiente.
func opBlock(t *testing.T, src, id string) string {
	t.Helper()
	marker := `OperationID: "` + id + `"`
	start := strings.Index(src, marker)
	if start < 0 {
		t.Fatalf("la tabla generada no contiene %s", id)
	}
	block := src[start+len(marker):]
	if end := strings.Index(block, "OperationID:"); end >= 0 {
		block = block[:end]
	}
	return strings.Join(strings.Fields(marker+block), " ")
}

// TestGeneratedMetadataOfRepresentativeDocumentTemplateOperations fija, para unas
// pocas operaciones nuevas representativas, los metadatos que el CLI necesita:
// método, cabeceras condicionales, descarga binaria o dual, multipart, noRetry y
// campo requerido y nullable. No es un snapshot de la tabla entera.
func TestGeneratedMetadataOfRepresentativeDocumentTemplateOperations(t *testing.T) {
	out, nonConforming, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(nonConforming) > 0 {
		t.Fatalf("operationId sin namespacear: %v", nonConforming)
	}
	src := string(out)

	cases := []struct {
		id   string
		want []string
	}{
		{"public-api.v1.series.update", []string{`Method: "PATCH"`, `Path: "/companies/{company}/series/{series}"`, `Action: "update"`, `RequiredScope: "series:write"`}},
		{"public-api.v1.pdf_templates.update", []string{`Method: "PATCH"`, `HeaderParams: []genParam{{Name: "If-Match"`, `ReturnsETag: true`}},
		{"public-api.v1.pdf_artifacts.download", []string{`BinaryContentType: "application/pdf"`, `Name: "If-None-Match"`, `ReturnsETag: true`}},
		{"public-api.v1.pdf_templates.asset_get", []string{`DualBinary: true`}},
		{"public-api.v1.pdf_templates.asset_upload", []string{`genBody{Kind: "multipart"`, `FileFields: []string{"file"}`, `IdempotencyRequired: true`}},
		{"public-api.v1.invoices.face_submissions.submit", []string{`NoRetry: true`, `Irreversible: true`}},
		{"public-api.v1.invoices.assign_presentation", []string{`Name: "template_id", Type: "string", Kind: "scalar", Required: true, Nullable: true`}},
		{"public-api.v1.invoices.list_presentations", []string{`Name: "page"`, `Name: "per_page"`}},
	}
	for _, tc := range cases {
		block := opBlock(t, src, tc.id)
		for _, want := range tc.want {
			if !strings.Contains(block, want) {
				t.Errorf("%s: falta %q en la tabla generada", tc.id, want)
			}
		}
	}

	// Un listado por offset (page/per_page) no es de cursor: no declara
	// starting_after, así que el CLI lo pide en una sola petición.
	if strings.Contains(opBlock(t, src, "public-api.v1.invoices.list_presentations"), "starting_after") {
		t.Error("list_presentations es offset: no debe declarar starting_after")
	}
	// Solo la descarga dual lleva DualBinary; una descarga que solo es binaria no.
	if strings.Contains(opBlock(t, src, "public-api.v1.pdf_artifacts.download"), "DualBinary") {
		t.Error("pdf_artifacts.download es solo binaria: no debe llevar DualBinary")
	}
	// Una propiedad con el esquema booleano `false` está reservada por el
	// contrato: no se ofrece como campo.
	if strings.Contains(opBlock(t, src, "public-api.v1.recurring_invoices.create"), `Name: "recurring_intent"`) {
		t.Error("recurring_intent es una propiedad prohibida (esquema false): no debe generarse como campo")
	}
}
