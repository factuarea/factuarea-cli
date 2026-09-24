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

// Las descargas binarias salen del SPEC, no de una lista de correcciones del
// cliente: el backend declara su Content-Type real y aquí solo se comprueba que
// llegan bien. Si una de estas vuelve a `nil`, el spec ha dejado de declararla
// (y el comando generado trataría los bytes como JSON).
func TestBinaryDownloadsComeFromTheSpec(t *testing.T) {
	ops, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	by := map[string]Operation{}
	for _, o := range ops {
		by[o.OperationID] = o
	}

	const xlsx = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

	for id, want := range map[string]string{
		"public-api.v1.quotes.pdf":                                "application/pdf",
		"public-api.v1.proformas.pdf":                             "application/pdf",
		"public-api.v1.invoices.pdf_preview":                      "application/pdf",
		"public-api.v1.purchase_invoices.payment_receipt":         "application/pdf",
		"public-api.v1.purchase_invoices.file":                    "application/pdf",
		"public-api.v1.tax_reports.download":                      "text/plain",
		"public-api.v1.invoices.export_excel":                     xlsx,
		"public-api.v1.monthly_time_record_closes.export":         xlsx,
		"public-api.v1.monthly_time_record_closes.payroll_export": xlsx,
	} {
		o := by[id]
		if o.BinaryResponse == nil || o.BinaryResponse.ContentType != want {
			t.Errorf("%s debe tener BinaryResponse %s desde el spec: %+v", id, want, o.BinaryResponse)
		}
	}

	// Un POST binario conserva su cuerpo de petición: el export de facturas
	// recibe los filtros en el body y devuelve la hoja de cálculo.
	if xl := by["public-api.v1.invoices.export_excel"]; xl.Body == nil || xl.Body.Kind != "json" {
		t.Errorf("invoices.export_excel debe conservar su body JSON: %+v", xl.Body)
	}
}

func TestOverridesFixSeriesDefault(t *testing.T) {
	ops, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	by := map[string]Operation{}
	for _, o := range ops {
		by[o.OperationID] = o
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

// TestPurchaseScanUploadDeclaresFileArray cubre D5 (`design.md`) para la
// subida del escáner: el nombre de campo congelado en `anchors/contract.md`
// §1.4 es `files[]` en la parte HTTP, pero el checkout YA implementaba el
// array de binarios con una lista separada (`FileArrayFields`, sin el sufijo
// `[]` en el propio nombre) en vez del sufijo `[]` sobre `FileFields` que
// describía `design.md` D5 literal (ver `anchors/factuarea-cli.md` "Hallazgo
// principal"). Este test mide el comportamiento YA shippeado, que sigue
// siendo D5-compatible (multipart de N ficheros bajo el mismo campo): no lo
// reescribe a la forma literal del diseño.
func TestPurchaseScanUploadDeclaresFileArray(t *testing.T) {
	ops, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	by := map[string]Operation{}
	for _, o := range ops {
		by[o.OperationID] = o
	}

	create := by["public-api.v1.purchase_scans.create"]
	if create.Body == nil || create.Body.Kind != "multipart" {
		t.Fatalf("purchase_scans.create debe ser multipart: %+v", create.Body)
	}
	if !contains(create.Body.FileArrayFields, "files") {
		t.Errorf("purchase_scans.create debe declarar FileArrayFields con el campo congelado en anchors/contract.md §1.4 (files, sin el sufijo []): %v", create.Body.FileArrayFields)
	}
}

// TestIdempotencyRequiredFromSpec cubre D6 (`design.md`): `IdempotencyRequired`
// se deriva del parámetro `in: header`, `name: Idempotency-Key`, `required:
// true`. Es solo informativo — `internal/client/client.go` ya autogenera la
// cabecera en todo verbo mutador — pero debe reflejar fielmente lo que
// declara el spec, congelado en `anchors/contract.md` §1.5.
func TestIdempotencyRequiredFromSpec(t *testing.T) {
	ops, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	by := map[string]Operation{}
	for _, o := range ops {
		by[o.OperationID] = o
	}

	for _, id := range []string{
		"public-api.v1.purchase_scans.archive",
		"public-api.v1.purchase_scans.create",
		"public-api.v1.purchase_scans.retry",
		"public-api.v1.purchase_scans.duplicate_resolution",
		"public-api.v1.purchase_scans.convert",
		"public-api.v1.purchase_scans.restore",
		// DELETE ajeno al escáner: la lista completa vive en anchors/contract.md §1.5.
		"public-api.v1.invoices.delete",
	} {
		if op, ok := by[id]; !ok || !op.IdempotencyRequired {
			t.Errorf("%s debe tener IdempotencyRequired=true", id)
		}
	}

	if op := by["public-api.v1.purchase_scans.list"]; op.IdempotencyRequired {
		t.Error("purchase_scans.list (GET) NO debe tener IdempotencyRequired")
	}
	if op := by["public-api.v1.purchase_scans.review"]; op.IdempotencyRequired {
		t.Error("purchase_scans.review NO debe tener IdempotencyRequired (medido no-requerida en anchors/contract.md §1.5)")
	}
}

// TestPurchaseScanOperationsResolve comprueba que las 13 operaciones del
// escáner resuelven a los grupos y a la acción de la tabla CLI de
// `design.md` (misma regla que `internal/spec/namespace.go`).
func TestPurchaseScanOperationsResolve(t *testing.T) {
	ops, _, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	by := map[string]Operation{}
	for _, o := range ops {
		by[o.OperationID] = o
	}

	cases := []struct {
		id     string
		groups []string
		action string
	}{
		{"public-api.v1.purchase_scans.list", []string{"purchase-scans"}, "list"},
		{"public-api.v1.purchase_scans.create", []string{"purchase-scans"}, "create"},
		{"public-api.v1.purchase_scans.stats", []string{"purchase-scans"}, "stats"},
		{"public-api.v1.purchase_scans.show", []string{"purchase-scans"}, "show"},
		{"public-api.v1.purchase_scans.source", []string{"purchase-scans"}, "source"},
		{"public-api.v1.purchase_scans.retry", []string{"purchase-scans"}, "retry"},
		{"public-api.v1.purchase_scans.review", []string{"purchase-scans"}, "review"},
		{"public-api.v1.purchase_scans.duplicate_resolution", []string{"purchase-scans"}, "duplicate-resolution"},
		{"public-api.v1.purchase_scans.convert", []string{"purchase-scans"}, "convert"},
		{"public-api.v1.purchase_scans.archive", []string{"purchase-scans"}, "archive"},
		{"public-api.v1.purchase_scans.restore", []string{"purchase-scans"}, "restore"},
		{"public-api.v1.purchase_scan_emails.list", []string{"purchase-scan-emails"}, "list"},
		{"public-api.v1.purchase_invoices.expense_categories", []string{"purchase-invoices"}, "expense-categories"},
	}
	if len(cases) != 13 {
		t.Fatalf("tabla de prueba incompleta: %d casos, want 13", len(cases))
	}
	for _, c := range cases {
		op, ok := by[c.id]
		if !ok {
			t.Errorf("%s ausente de Load()", c.id)
			continue
		}
		if !reflect.DeepEqual(op.Groups, c.groups) {
			t.Errorf("%s: Groups = %v, want %v", c.id, op.Groups, c.groups)
		}
		if op.Action != c.action {
			t.Errorf("%s: Action = %q, want %q", c.id, op.Action, c.action)
		}
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

	// `clients`/`suppliers` se retiran del spec pineado (re-pin desde el
	// export de `scanner-sdk-cli-spec-sync`, decisión del propietario
	// 2026-09-24, ver `anchors/contract.md` "Hallazgo crítico"): sustituidos
	// por `contacts`, que es lo que mide este test desde ahora.
	create := by["public-api.v1.contacts.create"]
	if create.Body == nil || len(create.Body.Fields) == 0 {
		t.Fatalf("contacts.create debe tener Body.Fields: %+v", create.Body)
	}
	fields := indexFields(create.Body.Fields)

	name := fields["name"]
	if name == nil || name.Kind != "scalar" || name.Type != "string" || !name.Required {
		t.Errorf("name debe ser scalar/string/required: %+v", name)
	}
	if name.Nullable {
		t.Errorf("name no es nullable: %+v", name)
	}

	taxID := fields["tax_id"]
	if taxID == nil || taxID.Kind != "scalar" || taxID.Type != "string" || !taxID.Nullable {
		t.Errorf("tax_id debe ser scalar/string/nullable: %+v", taxID)
	}

	altIDType := fields["alternative_id_type"]
	if altIDType == nil || len(altIDType.Enum) == 0 {
		t.Fatalf("alternative_id_type debe traer enum: %+v", altIDType)
	}
	if !contains(altIDType.Enum, "passport") {
		t.Errorf("alternative_id_type enum debe incluir passport: %v", altIDType.Enum)
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

	// Ver la nota de `TestBodyFieldsScalarEnumNested`: `clients` → `contacts`.
	contact := by["public-api.v1.contacts.create"]
	cf := indexFields(contact.Body.Fields)

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
