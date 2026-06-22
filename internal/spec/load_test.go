package spec

import (
	"reflect"
	"testing"
)

// Cobertura del parseo sobre el spec REAL embebido. Load() es lenient: parsea las
// operaciones conformes y devuelve las no conformes en nonConforming SIN abortar
// (solo da error si falla el parseo del spec). Verifica que el parser refleja el
// spec real.
func TestLoadParsesRealSpec(t *testing.T) {
	ops, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ops) < 200 {
		t.Fatalf("expected >=200 operations, got %d", len(ops))
	}

	by := map[string]Operation{}
	for _, o := range ops {
		by[o.OperationID] = o
	}

	// CRUD con body json + ejemplo
	create := by["public-api.v1.invoices.create"]
	if create.Method != "POST" || create.Body == nil || create.Body.Kind != "json" || create.Body.Example == "" {
		t.Errorf("invoices.create mal parseado: %+v", create)
	}
	if !create.Mutating() {
		t.Error("invoices.create debe ser mutating")
	}

	// list paginado (query param starting_after)
	list := by["public-api.v1.invoices.list"]
	if !list.Paginated() {
		t.Error("invoices.list debe detectarse como paginado (starting_after)")
	}

	// path param (en el spec real el GET de un recurso es .show, no .get)
	get := by["public-api.v1.invoices.show"]
	if len(get.PathParams) != 1 || get.PathParams[0].Name == "" {
		t.Errorf("invoices.show debe tener 1 path param: %+v", get.PathParams)
	}

	// respuesta binaria
	pdf := by["public-api.v1.invoices.pdf"]
	if pdf.BinaryResponse == nil || pdf.BinaryResponse.ContentType != "application/pdf" {
		t.Errorf("invoices.pdf debe tener BinaryResponse pdf: %+v", pdf.BinaryResponse)
	}

	// descarga no-JSON sin format:binary. Regla: 200 no-JSON = descarga. El
	// FacturaE XML tiene application/xml con schema {type:"string"} (sin
	// format:binary); aun así debe detectarse como descarga, no como JSON.
	facturae := by["public-api.v1.invoices.facturae"]
	if facturae.BinaryResponse == nil || facturae.BinaryResponse.ContentType != "application/xml" {
		t.Errorf("invoices.facturae debe tener BinaryResponse application/xml: %+v", facturae.BinaryResponse)
	}

	// multipart upload con campo binario
	up := by["public-api.v1.verifactu.certificates.upload"]
	if up.Body == nil || up.Body.Kind != "multipart" || len(up.Body.FileFields) == 0 {
		t.Errorf("certificates.upload debe ser multipart con FileFields: %+v", up.Body)
	}
}

// Baseline de drift de no-conformes. Hoy el spec real tiene UN único operationId
// que NO cumple el invariante public-api.v1.{recurso}.{accion}:
// `public-api.v1.payment_methods` (un solo segmento tras el prefijo, sin acción).
// Load() lo omite del árbol y lo reporta en nonConforming (el generador lo logea).
//
// Si aparece OTRO no-conforme nuevo en el spec, este test CASCA: caza el drift y
// obliga a una decisión consciente (nunca silenciar). Esta baseline solo se
// actualiza con decisión deliberada: cuando el backend namespacee `payment_methods`
// → `payment_methods.list`, pon `want := []string{}` (o nil); esta comparación
// tolera nil/empty (Load devuelve nil cuando no hay no-conformes, y
// reflect.DeepEqual(nil, []string{}) es false).
func TestNonConformingOperationsAreBaseline(t *testing.T) {
	_, nonConforming, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"public-api.v1.payment_methods"}
	if len(nonConforming) != len(want) || (len(want) > 0 && !reflect.DeepEqual(nonConforming, want)) {
		t.Fatalf("baseline de no-conformes cambió: got %v, want %v (¿drift? decide conscientemente)", nonConforming, want)
	}
}
