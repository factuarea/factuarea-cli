package trigger

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/factuarea/factuarea-cli/internal/client"
)

func TestRunClientCreated(t *testing.T) {
	var posted bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/clients") {
			posted = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"cli_1"}}`))
	}))
	defer srv.Close()
	c := client.New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa", client.WithBaseURL(srv.URL), client.WithSleep(func(time.Duration) {}))
	if err := Run(context.Background(), c, "client.created", nil); err != nil {
		t.Fatal(err)
	}
	if !posted {
		t.Fatal("client.created debe hacer POST /v1/clients")
	}
}

func TestRunUnsupported(t *testing.T) {
	c := client.New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	err := Run(context.Background(), c, "no.such_event", nil)
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
		case r.Method == http.MethodGet && r.URL.Path == "/v1/clients":
			_, _ = w.Write([]byte(`{"data":[{"id":"cli_1"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/series":
			_, _ = w.Write([]byte(`{"data":[{"id":"ser_1","is_default":false},{"id":"ser_2","is_default":true}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/taxes/active":
			_, _ = w.Write([]byte(`{"data":[{"id":"tax_1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/invoices":
			_, _ = w.Write([]byte(`{"data":{"id":"inv_1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/invoices/inv_1/mark-paid":
			_, _ = w.Write([]byte(`{"data":{"id":"inv_1","status":"paid"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"no encontrado"}}`))
		}
	}))
	defer srv.Close()
	c := client.New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa", client.WithBaseURL(srv.URL), client.WithSleep(func(time.Duration) {}))
	if err := Run(context.Background(), c, "invoice.paid", nil); err != nil {
		t.Fatal(err)
	}

	createIdx := indexOf(calls, "POST /v1/invoices")
	if createIdx < 0 {
		t.Fatalf("falta POST /v1/invoices; calls=%v", calls)
	}
	payIdx := indexOf(calls, "POST /v1/invoices/inv_1/mark-paid")
	if payIdx < 0 {
		t.Fatalf("falta POST /v1/invoices/inv_1/mark-paid; calls=%v", calls)
	}
	if payIdx < createIdx {
		t.Fatalf("mark-paid debe ir después de crear la factura; calls=%v", calls)
	}
	for _, dep := range []string{"GET /v1/clients", "GET /v1/series", "GET /v1/taxes/active"} {
		idx := indexOf(calls, dep)
		if idx < 0 || idx > createIdx {
			t.Fatalf("la dependencia %q debe resolverse antes de crear la factura; calls=%v", dep, calls)
		}
	}
}

func TestSupported(t *testing.T) {
	got := Supported()
	want := []string{
		"client.created",
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
