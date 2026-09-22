package spec

import (
	"reflect"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		id     string
		groups []string
		action string
		ok     bool
	}{
		{"public-api.v1.invoices.create", []string{"invoices"}, "create", true},
		{"public-api.v1.invoices.mark_paid", []string{"invoices"}, "mark-paid", true},
		{"public-api.v1.invoices.payments_create", []string{"invoices"}, "payments-create", true},
		{"public-api.v1.verifactu.records.find_by_csv", []string{"verifactu", "records"}, "find-by-csv", true},
		{"public-api.v1.products.gallery.upload", []string{"products", "gallery"}, "upload", true},
		{"public-api.v1.delivery_notes.list", []string{"delivery-notes"}, "list", true},
		{"public-api.v1.stripe_autoinvoicing.accounts.list", []string{"stripe-autoinvoicing", "accounts"}, "list", true},
		{"public-api.v1.webhook_endpoints.deliveries.replay", []string{"webhook-endpoints", "deliveries"}, "replay", true},
		// Eje de empresa: el recurso anidado resuelve igual que cualquier otro.
		{"public-api.v1.products.variants.list", []string{"products", "variants"}, "list", true},
		{"public-api.v1.products.presentations.create", []string{"products", "presentations"}, "create", true},
		// Eje de cuenta: mismo trato, y con la acción en snake_case.
		{"public-api.v1.account.api_keys.rotate_secret", []string{"account", "api-keys"}, "rotate-secret", true},
		{"public-api.v1.account.api_keys.revoke", []string{"account", "api-keys"}, "revoke", true},
		{"public-api.v1.account", nil, "", false},
		{"some.other.id", nil, "", false},
		{"public-api.v1", nil, "", false},
		{"public-api.v1.products", nil, "", false},
	}
	for _, c := range cases {
		g, a, ok := Resolve(c.id)
		if ok != c.ok || a != c.action || !reflect.DeepEqual(g, c.groups) {
			t.Errorf("Resolve(%q) = (%v,%q,%v), want (%v,%q,%v)", c.id, g, a, ok, c.groups, c.action, c.ok)
		}
	}
}

// specForOperationAtPath arma un contrato mínimo con UNA operación colgada del
// path que se le pase, con cada `{segmento}` como parámetro de ruta.
func specForOperationAtPath(opID, method, path string, params ...string) string {
	decls := make([]string, 0, len(params))
	for _, p := range params {
		decls = append(decls, `{"name": "`+p+`", "in": "path", "required": true, "schema": {"type": "string"}}`)
	}
	return `{
  "openapi": "3.1.0",
  "info": {"title": "contrato de prueba", "version": "1.0.0"},
  "paths": {
    "` + path + `": {
      "` + strings.ToLower(method) + `": {
        "operationId": "` + opID + `",
        "summary": "operación de prueba",
        "parameters": [` + strings.Join(decls, ", ") + `],
        "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "object"}}}}}
      }
    }
  }
}`
}

// specForPath es el atajo del caso original.
func specForPath(path string, params ...string) string {
	return specForOperationAtPath("public-api.v1.products.variants.list", "get", path, params...)
}

// loadSingleOperation carga un contrato de prueba EN LUGAR del spec embebido.
func loadSingleOperation(t *testing.T, raw string) Operation {
	t.Helper()
	original := Raw
	t.Cleanup(func() { Raw = original })
	Raw = []byte(raw)
	ops, nonConforming, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(nonConforming) != 0 {
		t.Fatalf("nonConforming = %v, want ninguno", nonConforming)
	}
	if len(ops) != 1 {
		t.Fatalf("ops = %d, want 1", len(ops))
	}
	return ops[0]
}

// TestCompanyAxisDoesNotMoveTheCommandTree fija el invariante del que depende.
func TestCompanyAxisDoesNotMoveTheCommandTree(t *testing.T) {
	flat := loadSingleOperation(t, specForPath("/products/{product}/variants", "product"))
	scoped := loadSingleOperation(t, specForPath("/companies/{company}/products/{product}/variants", "company", "product"))

	if !reflect.DeepEqual(flat.Groups, scoped.Groups) {
		t.Fatalf("grupos distintos con y sin el segmento de empresa: %v vs %v", flat.Groups, scoped.Groups)
	}
	if !reflect.DeepEqual(flat.Groups, []string{"products", "variants"}) {
		t.Fatalf("grupos = %v, want [products variants]", flat.Groups)
	}
	if flat.Action != scoped.Action || flat.Action != "list" {
		t.Fatalf("acciones distintas: %q vs %q", flat.Action, scoped.Action)
	}

	if names := pathParamNames(flat); !reflect.DeepEqual(names, []string{"product"}) {
		t.Fatalf("parámetros de ruta del path plano = %v, want [product]", names)
	}
	if names := pathParamNames(scoped); !reflect.DeepEqual(names, []string{"company", "product"}) {
		t.Fatalf("parámetros de ruta del path con empresa = %v, want [company product]", names)
	}
}

func pathParamNames(o Operation) []string {
	names := make([]string, 0, len(o.PathParams))
	for _, p := range o.PathParams {
		names = append(names, p.Name)
	}
	return names
}

// axisRepathCase es un par de paths MEDIDOS sobre los DOS contratos reales.
type axisRepathCase struct {
	opID       string
	method     string
	flatPath   string
	flatParams []string
	axisPath   string
	axisParams []string
	groups     []string
	action     string
}

// TestSameOperationIDResolvesTheSameWhateverThePath fija el anclaje central.
func TestSameOperationIDResolvesTheSameWhateverThePath(t *testing.T) {
	cases := []axisRepathCase{
		{
			opID: "public-api.v1.invoices.show", method: "get",
			flatPath: "/invoices/{invoice}", flatParams: []string{"invoice"},
			axisPath: "/companies/{company}/invoices/{invoice}", axisParams: []string{"company", "invoice"},
			groups: []string{"invoices"}, action: "show",
		},
		{
			opID: "public-api.v1.invoices.mark_paid", method: "post",
			flatPath: "/invoices/{invoice}/mark-paid", flatParams: []string{"invoice"},
			axisPath: "/companies/{company}/invoices/{invoice}/mark-paid", axisParams: []string{"company", "invoice"},
			groups: []string{"invoices"}, action: "mark-paid",
		},
		{
			opID: "public-api.v1.invoices.payments_create", method: "post",
			flatPath: "/invoices/{invoice}/payments", flatParams: []string{"invoice"},
			axisPath: "/companies/{company}/invoices/{invoice}/payments", axisParams: []string{"company", "invoice"},
			groups: []string{"invoices"}, action: "payments-create",
		},
		{
			// Recurso anidado de dos grupos y CERO parámetros de ruta en el contrato.
			opID: "public-api.v1.verifactu.records.find_by_csv", method: "post",
			flatPath: "/verifactu/records/find-by-csv", flatParams: []string{},
			axisPath: "/companies/{company}/verifactu/records/find-by-csv", axisParams: []string{"company"},
			groups: []string{"verifactu", "records"}, action: "find-by-csv",
		},
		{
			// Además de ganar el eje, el path cambia de ortografía (`/delivery_notes`.
			opID: "public-api.v1.delivery_notes.list", method: "get",
			flatPath: "/delivery_notes", flatParams: []string{},
			axisPath: "/companies/{company}/delivery-notes", axisParams: []string{"company"},
			groups: []string{"delivery-notes"}, action: "list",
		},
	}

	for _, c := range cases {
		t.Run(c.opID, func(t *testing.T) {
			groups, action, ok := Resolve(c.opID)
			if !ok || action != c.action || !reflect.DeepEqual(groups, c.groups) {
				t.Fatalf("Resolve(%q) = (%v,%q,%v), want (%v,%q,true)", c.opID, groups, action, ok, c.groups, c.action)
			}

			flat := loadSingleOperation(t, specForOperationAtPath(c.opID, c.method, c.flatPath, c.flatParams...))
			axis := loadSingleOperation(t, specForOperationAtPath(c.opID, c.method, c.axisPath, c.axisParams...))

			if !reflect.DeepEqual(flat.Groups, axis.Groups) || !reflect.DeepEqual(flat.Groups, c.groups) {
				t.Fatalf("grupos: plano %v, con eje %v, want %v", flat.Groups, axis.Groups, c.groups)
			}
			if flat.Action != axis.Action || flat.Action != c.action {
				t.Fatalf("acciones: plano %q, con eje %q, want %q", flat.Action, axis.Action, c.action)
			}
			if flat.Path == axis.Path {
				t.Fatalf("el caso no prueba nada: los dos paths son el mismo (%q)", flat.Path)
			}
			if flat.Path != c.flatPath || axis.Path != c.axisPath {
				t.Fatalf("paths cargados = (%q, %q), want (%q, %q)", flat.Path, axis.Path, c.flatPath, c.axisPath)
			}
			if flat.Method != axis.Method || flat.Method != strings.ToUpper(c.method) {
				t.Fatalf("métodos: plano %q, con eje %q, want %q", flat.Method, axis.Method, strings.ToUpper(c.method))
			}
			if names := pathParamNames(flat); !reflect.DeepEqual(names, c.flatParams) {
				t.Fatalf("parámetros de ruta del path plano = %v, want %v", names, c.flatParams)
			}
			// El re-path añade el eje y NADA más, y lo añade delante.
			wantAxis := append([]string{"company"}, c.flatParams...)
			if names := pathParamNames(axis); !reflect.DeepEqual(names, wantAxis) {
				t.Fatalf("parámetros de ruta del path con eje = %v, want %v", names, wantAxis)
			}
		})
	}
}

// TestResolveRejectsIdentifiersOutsideTheV1Namespace fija las DOS puertas de
// `Resolve`: el prefijo canónico y los dos segmentos mínimos.
func TestResolveRejectsIdentifiersOutsideTheV1Namespace(t *testing.T) {
	// Sin el prefijo `public-api.v1.`, en todas sus formas de «casi».
	withoutPrefix := []string{
		"",
		"invoices.list",
		"public-api.invoices.list",
		"public-api.v2.invoices.list",
		"public-api.v10.invoices.list",
		"public-api.v1invoices.list",
		"PUBLIC-API.V1.invoices.list",
		" public-api.v1.invoices.list",
		"api.public-api.v1.invoices.list",
		"/companies/{company}/invoices",
	}
	// Con el prefijo, pero con menos de dos segmentos detrás.
	tooFewSegments := []string{
		"public-api.v1",
		"public-api.v1.",
		"public-api.v1.invoices",
		"public-api.v1.account",
		"public-api.v1.products",
	}

	for _, group := range []struct {
		why string
		ids []string
	}{
		{"sin el prefijo del espacio de nombres de la v1", withoutPrefix},
		{"con menos de dos segmentos tras el prefijo", tooFewSegments},
	} {
		for _, id := range group.ids {
			groups, action, ok := Resolve(id)
			if ok || action != "" || groups != nil {
				t.Errorf("Resolve(%q) = (%v,%q,%v), want (nil,\"\",false) — %s", id, groups, action, ok, group.why)
			}
		}
	}

	raw := specForOperationAtPath("legacy.contacts.list", "get", "/companies/{company}/contacts", "company")
	original := Raw
	t.Cleanup(func() { Raw = original })
	Raw = []byte(raw)
	ops, nonConforming, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ops) != 0 {
		t.Fatalf("ops = %d, want 0: un identificador no conforme no puede producir comando", len(ops))
	}
	if !reflect.DeepEqual(nonConforming, []string{"legacy.contacts.list"}) {
		t.Fatalf("nonConforming = %v, want [legacy.contacts.list]: el censo no puede depender del path", nonConforming)
	}
}

// TestKebabConversionComesFromTheOperationIDNotThePath fija que el guion bajo.
func TestKebabConversionComesFromTheOperationIDNotThePath(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"delivery_notes", "delivery-notes"},
		{"mark_paid", "mark-paid"},
		{"find_by_csv", "find-by-csv"},
		{"stripe_autoinvoicing", "stripe-autoinvoicing"},
		{"already-kebab", "already-kebab"},
		{"a__b", "a--b"},
		{"", ""},
	} {
		if got := ToKebab(c.in); got != c.want {
			t.Errorf("ToKebab(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// Guiones bajos en los DOS grupos y en la acción a la vez.
	groups, action, ok := Resolve("public-api.v1.webhook_endpoints.failed_deliveries.retry_all")
	if !ok || !reflect.DeepEqual(groups, []string{"webhook-endpoints", "failed-deliveries"}) || action != "retry-all" {
		t.Fatalf("Resolve(...retry_all) = (%v,%q,%v), want ([webhook-endpoints failed-deliveries],\"retry-all\",true)", groups, action, ok)
	}

	const id = "public-api.v1.delivery_notes.list"
	paths := []struct {
		path   string
		params []string
	}{
		{"/delivery_notes", []string{}},                                // contrato pineado hoy
		{"/companies/{company}/delivery-notes", []string{"company"}},   // contrato congelado
		{"/DELIVERY_NOTES/{delivery_note}", []string{"delivery_note"}}, // path hostil
	}
	for _, p := range paths {
		op := loadSingleOperation(t, specForOperationAtPath(id, "get", p.path, p.params...))
		if !reflect.DeepEqual(op.Groups, []string{"delivery-notes"}) || op.Action != "list" {
			t.Fatalf("en %q: grupos %v acción %q, want [delivery-notes] \"list\"", p.path, op.Groups, op.Action)
		}
		if names := pathParamNames(op); !reflect.DeepEqual(names, p.params) {
			t.Fatalf("en %q: parámetros de ruta %v, want %v (ToKebab no toca los parámetros)", p.path, names, p.params)
		}
	}
}
