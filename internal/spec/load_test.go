package spec

import (
	"reflect"
	"testing"
)

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

	create := by["public-api.v1.invoices.create"]
	if create.Method != "POST" || create.Body == nil || create.Body.Kind != "json" || create.Body.Example == "" {
		t.Errorf("invoices.create mal parseado: %+v", create)
	}
	if !create.Mutating() {
		t.Error("invoices.create debe ser mutating")
	}

	list := by["public-api.v1.invoices.list"]
	if !list.Paginated() {
		t.Error("invoices.list debe detectarse como paginado (starting_after)")
	}

	get := by["public-api.v1.invoices.show"]
	if len(get.PathParams) != 1 || get.PathParams[0].Name == "" {
		t.Errorf("invoices.show debe tener 1 path param: %+v", get.PathParams)
	}

	pdf := by["public-api.v1.invoices.pdf"]
	if pdf.BinaryResponse == nil || pdf.BinaryResponse.ContentType != "application/pdf" {
		t.Errorf("invoices.pdf debe tener BinaryResponse pdf: %+v", pdf.BinaryResponse)
	}

	facturae := by["public-api.v1.invoices.facturae"]
	if facturae.BinaryResponse == nil || facturae.BinaryResponse.ContentType != "application/xml" {
		t.Errorf("invoices.facturae debe tener BinaryResponse application/xml: %+v", facturae.BinaryResponse)
	}

	up := by["public-api.v1.verifactu.certificates.upload"]
	if up.Body == nil || up.Body.Kind != "multipart" || len(up.Body.FileFields) == 0 {
		t.Errorf("certificates.upload debe ser multipart con FileFields: %+v", up.Body)
	}
}

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
