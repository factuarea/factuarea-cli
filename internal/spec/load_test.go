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

func TestOverridesFixBinaryDownloadsAndSeriesDefault(t *testing.T) {
	ops, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	by := map[string]Operation{}
	for _, o := range ops {
		by[o.OperationID] = o
	}

	for _, id := range []string{
		"public-api.v1.quotes.pdf",
		"public-api.v1.proformas.pdf",
		"public-api.v1.tax_reports.download",
		"public-api.v1.invoices.pdf_preview",
	} {
		o := by[id]
		if o.BinaryResponse == nil || o.BinaryResponse.ContentType != "application/pdf" {
			t.Errorf("%s debe tener BinaryResponse application/pdf (override): %+v", id, o.BinaryResponse)
		}
		if o.Body != nil {
			t.Errorf("%s no debe tener body tras el override: %+v", id, o.Body)
		}
	}

	sd := by["public-api.v1.series.default"]
	var found *Param
	for i := range sd.QueryParams {
		if sd.QueryParams[i].Name == "document_type" {
			found = &sd.QueryParams[i]
		}
	}
	if found == nil {
		t.Fatalf("series.default debe exponer el query param document_type: %+v", sd.QueryParams)
	}
	if !found.Required {
		t.Errorf("document_type debe marcarse como requerido")
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

func TestBodyFieldsScalarEnumNested(t *testing.T) {
	ops, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	by := map[string]Operation{}
	for _, o := range ops {
		by[o.OperationID] = o
	}

	create := by["public-api.v1.clients.create"]
	if create.Body == nil || len(create.Body.Fields) == 0 {
		t.Fatalf("clients.create debe tener Body.Fields: %+v", create.Body)
	}
	fields := indexFields(create.Body.Fields)

	name := fields["name"]
	if name == nil || name.Kind != "scalar" || name.Type != "string" || !name.Required {
		t.Errorf("name debe ser scalar/string/required: %+v", name)
	}
	if name.Nullable {
		t.Errorf("name no es nullable: %+v", name)
	}

	terms := fields["payment_terms_days"]
	if terms == nil || terms.Kind != "scalar" || terms.Type != "integer" || !terms.Nullable {
		t.Errorf("payment_terms_days debe ser scalar/integer/nullable: %+v", terms)
	}

	pm := fields["payment_method"]
	if pm == nil || len(pm.Enum) == 0 {
		t.Fatalf("payment_method debe traer enum: %+v", pm)
	}
	if !contains(pm.Enum, "direct_debit") {
		t.Errorf("payment_method enum debe incluir direct_debit: %v", pm.Enum)
	}

	addr := fields["address"]
	if addr == nil || addr.Kind != "object" {
		t.Fatalf("address debe ser object: %+v", addr)
	}
	children := indexFields(addr.Children)
	if city := children["city"]; city == nil || city.Kind != "scalar" || city.Type != "string" {
		t.Errorf("address.city debe ser scalar/string: %+v", city)
	}
}

func TestBodyFieldsArraysAndMap(t *testing.T) {
	ops, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	by := map[string]Operation{}
	for _, o := range ops {
		by[o.OperationID] = o
	}

	client := by["public-api.v1.clients.create"]
	cf := indexFields(client.Body.Fields)

	emails := cf["billing_emails"]
	if emails == nil || emails.Kind != "scalar_array" || emails.Type != "string" {
		t.Errorf("billing_emails debe ser scalar_array de string: %+v", emails)
	}

	meta := cf["metadata"]
	if meta == nil || meta.Kind != "map" {
		t.Errorf("metadata debe ser map: %+v", meta)
	}

	banks := cf["bank_accounts"]
	if banks == nil || banks.Kind != "object_array" {
		t.Errorf("bank_accounts debe ser object_array: %+v", banks)
	}
	if len(banks.Children) != 0 {
		t.Errorf("object_array NO debe expandir Children: %+v", banks.Children)
	}

	inv := by["public-api.v1.invoices.create"]
	invf := indexFields(inv.Body.Fields)
	lines := invf["lines"]
	if lines == nil || lines.Kind != "object_array" {
		t.Errorf("invoices lines debe ser object_array: %+v", lines)
	}
	if len(lines.Children) != 0 {
		t.Errorf("lines object_array NO debe expandir Children: %+v", lines.Children)
	}
	if cid := invf["client_id"]; cid == nil || !cid.Required {
		t.Errorf("client_id debe ser required: %+v", cid)
	}
}

func TestBodyFieldsEmptyForNonTypedBody(t *testing.T) {
	ops, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	by := map[string]Operation{}
	for _, o := range ops {
		by[o.OperationID] = o
	}

	show := by["public-api.v1.invoices.show"]
	if show.Body != nil {
		t.Errorf("invoices.show no tiene body: %+v", show.Body)
	}

	up := by["public-api.v1.verifactu.certificates.upload"]
	if up.Body == nil || up.Body.Kind != "multipart" {
		t.Fatalf("certificates.upload debe ser multipart: %+v", up.Body)
	}
	if len(up.Body.Fields) != 0 {
		t.Errorf("multipart no debe poblar Fields: %+v", up.Body.Fields)
	}
}

func indexFields(fields []BodyField) map[string]*BodyField {
	m := map[string]*BodyField{}
	for i := range fields {
		m[fields[i].Name] = &fields[i]
	}
	return m
}

func contains(vals []string, want string) bool {
	for _, v := range vals {
		if v == want {
			return true
		}
	}
	return false
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
