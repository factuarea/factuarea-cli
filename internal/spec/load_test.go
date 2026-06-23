package spec

import (
	"reflect"
	"testing"

	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
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
	var want []string
	if len(nonConforming) != len(want) || (len(want) > 0 && !reflect.DeepEqual(nonConforming, want)) {
		t.Fatalf("baseline de no-conformes cambió: got %v, want %v (¿drift? decide conscientemente)", nonConforming, want)
	}
}

func TestLoadParsesOperationMetadata(t *testing.T) {
	ops, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	by := map[string]Operation{}
	for _, o := range ops {
		by[o.OperationID] = o
	}

	del := by["public-api.v1.invoices.delete"]
	if del.RequiredScope != "invoices:delete" {
		t.Errorf("invoices.delete RequiredScope = %q, want invoices:delete", del.RequiredScope)
	}
	if !del.Irreversible {
		t.Error("invoices.delete debe ser Irreversible (x-irreversible: true)")
	}

	show := by["public-api.v1.invoices.show"]
	if show.RequiredScope != "invoices:read" {
		t.Errorf("invoices.show RequiredScope = %q, want invoices:read", show.RequiredScope)
	}
	if show.Irreversible {
		t.Error("invoices.show NO debe ser Irreversible")
	}
}

func TestStringExtAndBoolExt(t *testing.T) {
	ext := orderedmap.New[string, *yaml.Node]()
	ext.Set("x-required-scope", strNode("invoices:read"))
	ext.Set("x-irreversible", boolNode(true))
	ext.Set("x-irreversible-false", boolNode(false))
	ext.Set("x-not-a-bool", strNode("nope"))

	if got := stringExt(ext, "x-required-scope"); got != "invoices:read" {
		t.Errorf("stringExt present = %q, want invoices:read", got)
	}
	if got := stringExt(ext, "x-missing"); got != "" {
		t.Errorf("stringExt missing = %q, want empty", got)
	}
	if got := stringExt(nil, "x-required-scope"); got != "" {
		t.Errorf("stringExt nil map = %q, want empty", got)
	}

	if !boolExt(ext, "x-irreversible") {
		t.Error("boolExt true scalar must be true")
	}
	if boolExt(ext, "x-irreversible-false") {
		t.Error("boolExt false scalar must be false")
	}
	if boolExt(ext, "x-missing") {
		t.Error("boolExt missing must default false")
	}
	if boolExt(ext, "x-not-a-bool") {
		t.Error("boolExt non-bool scalar must default false")
	}
	if boolExt(nil, "x-irreversible") {
		t.Error("boolExt nil map must default false")
	}
}

func strNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func boolNode(value bool) *yaml.Node {
	v := "false"
	if value {
		v = "true"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: v}
}
