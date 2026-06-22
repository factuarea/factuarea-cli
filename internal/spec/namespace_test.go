package spec

import (
	"reflect"
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
		{"public-api.v1.invoices.payments_create", []string{"invoices"}, "payments-create", true},              // underscore plano: NO promueve sub-nivel
		{"public-api.v1.verifactu.records.find_by_csv", []string{"verifactu", "records"}, "find-by-csv", true}, // sub-nivel real
		{"public-api.v1.products.gallery.upload", []string{"products", "gallery"}, "upload", true},
		{"public-api.v1.delivery_notes.list", []string{"delivery-notes"}, "list", true},
		{"public-api.v1.stripe_autoinvoicing.accounts.list", []string{"stripe-autoinvoicing", "accounts"}, "list", true},
		{"public-api.v1.webhook_endpoints.deliveries.replay", []string{"webhook-endpoints", "deliveries"}, "replay", true},
		{"public-api.v1.account", nil, "", false}, // <2 segmentos
		{"some.other.id", nil, "", false}, // sin prefijo
	}
	for _, c := range cases {
		g, a, ok := Resolve(c.id)
		if ok != c.ok || a != c.action || !reflect.DeepEqual(g, c.groups) {
			t.Errorf("Resolve(%q) = (%v,%q,%v), want (%v,%q,%v)", c.id, g, a, ok, c.groups, c.action, c.ok)
		}
	}
}
