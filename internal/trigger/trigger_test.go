package trigger

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/factuarea/factuarea-cli/internal/client"
)

func TestRunContactCreated(t *testing.T) {
	var posted bool
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, testBase+"/contacts") {
			posted = true
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"0199152d-525d-7000-8000-000000000001"}}`))
	}))
	defer srv.Close()
	c := client.New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa", client.WithBaseURL(srv.URL), client.WithSleep(func(time.Duration) {}))
	if err := Run(context.Background(), c, testBase, "contact.created", nil); err != nil {
		t.Fatal(err)
	}
	if !posted {
		t.Fatal("contact.created debe hacer POST " + testBase + "/contacts")
	}
	roles, ok := body["roles"].([]any)
	if !ok || len(roles) != 1 || roles[0] != "customer" {
		t.Fatalf("el contacto del fixture debe nacer con rol customer: %v", body["roles"])
	}
	if body["kind"] != "person" || body["name"] == "" {
		t.Fatalf("el cuerpo debe traer los requeridos name/kind: %v", body)
	}
}

func TestRunUnsupported(t *testing.T) {
	c := client.New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	err := Run(context.Background(), c, testBase, "no.such_event", nil)
	if err == nil || !strings.Contains(err.Error(), "soportado") {
		t.Fatalf("evento no soportado debe dar error con la lista; got %v", err)
	}
	if !strings.Contains(err.Error(), "invoice.paid") {
		t.Fatalf("el error debe listar los soportados; got %v", err)
	}
}

func TestRunInvoicePaidOrchestration(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == testBase+"/contacts":
			_, _ = w.Write([]byte(`{"data":[{"id":"0199152d-525d-7000-8000-000000000001"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == testBase+"/series":
			_, _ = w.Write([]byte(`{"data":[{"id":"ser_1","is_default":false},{"id":"ser_2","is_default":true}]}`))
		case r.Method == http.MethodGet && r.URL.Path == testBase+"/taxes/active":
			_, _ = w.Write([]byte(`{"data":[{"id":"tax_1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == testBase+"/invoices":
			_, _ = w.Write([]byte(`{"data":{"id":"inv_1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == testBase+"/invoices/inv_1/mark-sent":
			_, _ = w.Write([]byte(`{"data":{"id":"inv_1","status":"sent"}}`))
		case r.Method == http.MethodPost && r.URL.Path == testBase+"/invoices/inv_1/mark-paid":
			_, _ = w.Write([]byte(`{"data":{"id":"inv_1","status":"paid"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"no encontrado"}}`))
		}
	}))
	defer srv.Close()
	c := client.New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa", client.WithBaseURL(srv.URL), client.WithSleep(func(time.Duration) {}))
	if err := Run(context.Background(), c, testBase, "invoice.paid", nil); err != nil {
		t.Fatal(err)
	}

	createIdx := indexOf(calls, "POST "+testBase+"/invoices")
	if createIdx < 0 {
		t.Fatalf("falta POST %s/invoices; calls=%v", testBase, calls)
	}
	sentIdx := indexOf(calls, "POST "+testBase+"/invoices/inv_1/mark-sent")
	if sentIdx < 0 {
		t.Fatalf("falta POST %s/invoices/inv_1/mark-sent; calls=%v", testBase, calls)
	}
	payIdx := indexOf(calls, "POST "+testBase+"/invoices/inv_1/mark-paid")
	if payIdx < 0 {
		t.Fatalf("falta POST %s/invoices/inv_1/mark-paid; calls=%v", testBase, calls)
	}
	if !(createIdx < sentIdx && sentIdx < payIdx) {
		t.Fatalf("orden esperado crear < mark-sent < mark-paid; calls=%v", calls)
	}
	for _, dep := range []string{"GET " + testBase + "/contacts", "GET " + testBase + "/series", "GET " + testBase + "/taxes/active"} {
		idx := indexOf(calls, dep)
		if idx < 0 || idx > createIdx {
			t.Fatalf("la dependencia %q debe resolverse antes de crear la factura; calls=%v", dep, calls)
		}
	}
}

func TestSupported(t *testing.T) {
	got := Supported()
	want := []string{
		"contact.created",
		"invoice.created",
		"invoice.paid",
		"invoice.sent",
		"product.created",
		"quote.approved",
		"quote.created",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Supported() = %v, want %v", got, want)
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// testBase es el prefijo del EJE DE EMPRESA con el que el devloop opera: desde
// el eje, ninguna escritura de la v1 cuelga de una ruta plana.
const testBase = "/v1/companies/acme_co"
