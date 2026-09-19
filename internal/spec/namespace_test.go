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
		// Eje de empresa: el recurso anidado del ERP resuelve igual que
		// cualquier otro. El segmento `/companies/{company}/` del path NO
		// aparece aquí porque `Resolve` no ve el path: manda el nombre de ruta.
		{"public-api.v1.sales_orders.lines.list", []string{"sales-orders", "lines"}, "list", true},
		{"public-api.v1.sales_orders.convert_to_delivery_note", []string{"sales-orders"}, "convert-to-delivery-note", true},
		{"public-api.v1.purchase_orders.receipts.create", []string{"purchase-orders", "receipts"}, "create", true},
		{"public-api.v1.companies.api_keys.revoke", []string{"companies", "api-keys"}, "revoke", true},
		{"public-api.v1.account", nil, "", false},
		{"some.other.id", nil, "", false},
		{"public-api.v1", nil, "", false},
		{"public-api.v1.sales_orders", nil, "", false},
	}
	for _, c := range cases {
		g, a, ok := Resolve(c.id)
		if ok != c.ok || a != c.action || !reflect.DeepEqual(g, c.groups) {
			t.Errorf("Resolve(%q) = (%v,%q,%v), want (%v,%q,%v)", c.id, g, a, ok, c.groups, c.action, c.ok)
		}
	}
}

// specForPath arma un contrato mínimo con UNA operación de recurso anidado del
// ERP (`sales_orders.lines.list`) en el path que se le pase, declarando cada
// `{segmento}` como parámetro de ruta obligatorio.
func specForPath(path string, params ...string) string {
	decls := make([]string, 0, len(params))
	for _, p := range params {
		decls = append(decls, `{"name": "`+p+`", "in": "path", "required": true, "schema": {"type": "string"}}`)
	}
	return `{
  "openapi": "3.1.0",
  "info": {"title": "contrato de prueba", "version": "1.0.0"},
  "paths": {
    "` + path + `": {
      "get": {
        "operationId": "public-api.v1.sales_orders.lines.list",
        "summary": "Lista las líneas del pedido",
        "parameters": [` + strings.Join(decls, ", ") + `],
        "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "object"}}}}}
      }
    }
  }
}`
}

// TestCompanyAxisDoesNotMoveTheCommandTree fija el invariante del que depende el
// eje de empresa del contrato v1: el árbol de comandos lo gobierna el NOMBRE de
// la ruta (`operationId`), no el path. La misma operación anidada resuelve el
// mismo grupo y la misma acción con el segmento de empresa y sin él; lo único
// que cambia es que el path scopeado declara un parámetro de ruta MÁS, que es
// exactamente lo que el CLI convierte en posicional opcional cuando llega por
// la flag persistente `--company`.
func TestCompanyAxisDoesNotMoveTheCommandTree(t *testing.T) {
	original := Raw
	t.Cleanup(func() { Raw = original })

	load := func(t *testing.T, raw string) Operation {
		t.Helper()
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

	flat := load(t, specForPath("/sales-orders/{sales_order}/lines", "sales_order"))
	scoped := load(t, specForPath("/companies/{company}/sales-orders/{sales_order}/lines", "company", "sales_order"))

	if !reflect.DeepEqual(flat.Groups, scoped.Groups) {
		t.Fatalf("grupos distintos con y sin el segmento de empresa: %v vs %v", flat.Groups, scoped.Groups)
	}
	if !reflect.DeepEqual(flat.Groups, []string{"sales-orders", "lines"}) {
		t.Fatalf("grupos = %v, want [sales-orders lines]", flat.Groups)
	}
	if flat.Action != scoped.Action || flat.Action != "list" {
		t.Fatalf("acciones distintas: %q vs %q", flat.Action, scoped.Action)
	}

	if names := pathParamNames(flat); !reflect.DeepEqual(names, []string{"sales_order"}) {
		t.Fatalf("parámetros de ruta del path plano = %v, want [sales_order]", names)
	}
	if names := pathParamNames(scoped); !reflect.DeepEqual(names, []string{"company", "sales_order"}) {
		t.Fatalf("parámetros de ruta del path con empresa = %v, want [company sales_order]", names)
	}
}

func pathParamNames(o Operation) []string {
	names := make([]string, 0, len(o.PathParams))
	for _, p := range o.PathParams {
		names = append(names, p.Name)
	}
	return names
}
