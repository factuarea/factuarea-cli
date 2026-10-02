package trigger

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/factuarea/factuarea-cli/internal/client"
)

func TestRunContactCreated(t *testing.T) {
	var posted bool
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v1/contacts") {
			posted = true
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"0199152d-525d-7000-8000-000000000001"}}`))
	}))
	defer srv.Close()
	c := client.New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa", client.WithBaseURL(srv.URL), client.WithSleep(func(time.Duration) {}))
	if err := Run(context.Background(), c, "contact.created", nil); err != nil {
		t.Fatal(err)
	}
	if !posted {
		t.Fatal("contact.created debe hacer POST /v1/contacts")
	}
	roles, ok := body["roles"].([]any)
	if !ok || len(roles) != 1 || roles[0] != "customer" {
		t.Fatalf("el contacto del fixture debe nacer con rol customer: %v", body["roles"])
	}
	if body["kind"] != "person" || body["name"] == "" {
		t.Fatalf("el cuerpo debe traer los requeridos name/kind: %v", body)
	}
}

func TestRunProjectCreated(t *testing.T) {
	var calls []string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"0199152d-525d-7000-8000-0000000000a1"}}`))
	}))
	defer srv.Close()
	c := client.New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa", client.WithBaseURL(srv.URL), client.WithSleep(func(time.Duration) {}))
	if err := Run(context.Background(), c, "project.created", nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"POST /v1/projects"}) {
		t.Fatalf("project.created debe limitarse a POST /v1/projects; calls=%v", calls)
	}
	if name, _ := body["name"].(string); name == "" {
		t.Fatalf("el cuerpo debe traer el requerido name: %v", body)
	}
	key, _ := body["key"].(string)
	if !regexp.MustCompile(`^[A-Z][A-Z0-9]{0,9}$`).MatchString(key) {
		t.Fatalf("la clave del proyecto %q no cumple ^[A-Z][A-Z0-9]{0,9}$", key)
	}
}

func TestRunProjectCreatedHonoursOverrides(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"0199152d-525d-7000-8000-0000000000a1"}}`))
	}))
	defer srv.Close()
	c := client.New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa", client.WithBaseURL(srv.URL), client.WithSleep(func(time.Duration) {}))
	if err := Run(context.Background(), c, "project.created", map[string]string{"name": "Web", "key": "WEB"}); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "Web" || body["key"] != "WEB" {
		t.Fatalf("los overrides name/key deben llegar al cuerpo: %v", body)
	}
}

func TestUniqueProjectKeyIsValidAndChangesWithTheClock(t *testing.T) {
	valid := regexp.MustCompile(`^[A-Z][A-Z0-9]{0,9}$`)
	first := uniqueProjectKey(time.UnixMilli(1_790_000_000_000))
	second := uniqueProjectKey(time.UnixMilli(1_790_000_000_001))
	for _, key := range []string{first, second} {
		if !valid.MatchString(key) {
			t.Fatalf("la clave %q no cumple ^[A-Z][A-Z0-9]{0,9}$", key)
		}
	}
	if first == second {
		t.Fatalf("dos disparos consecutivos no pueden compartir clave: %q", first)
	}
}

func TestRunTaskCreatedOrchestration(t *testing.T) {
	var calls []string
	var taskBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects":
			_, _ = w.Write([]byte(`{"data":{"id":"0199152d-525d-7000-8000-0000000000a1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks":
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &taskBody)
			_, _ = w.Write([]byte(`{"data":{"id":"0199152d-525d-7000-8000-0000000000b1"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"no encontrado"}}`))
		}
	}))
	defer srv.Close()
	c := client.New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa", client.WithBaseURL(srv.URL), client.WithSleep(func(time.Duration) {}))
	if err := Run(context.Background(), c, "task.created", nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(calls, []string{"POST /v1/projects", "POST /v1/tasks"}) {
		t.Fatalf("orden esperado proyecto y después tarea; calls=%v", calls)
	}
	if taskBody["project_id"] != "0199152d-525d-7000-8000-0000000000a1" {
		t.Fatalf("la tarea debe crearse en el proyecto recién creado: %v", taskBody)
	}
	if title, _ := taskBody["title"].(string); title == "" {
		t.Fatalf("el cuerpo de la tarea debe traer el requerido title: %v", taskBody)
	}
}

func TestRunTaskCreatedStopsWhenTheProjectFails(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"project_key_in_use","message":"La clave ya está en uso."}}`))
	}))
	defer srv.Close()
	c := client.New("fact_test_aaaaaaaaaaaaaaaaaaaaaaaa", client.WithBaseURL(srv.URL), client.WithSleep(func(time.Duration) {}))
	if err := Run(context.Background(), c, "task.created", nil); err == nil {
		t.Fatal("si el proyecto no se crea, task.created debe devolver el error")
	}
	if !reflect.DeepEqual(calls, []string{"POST /v1/projects"}) {
		t.Fatalf("sin proyecto no se debe intentar crear la tarea; calls=%v", calls)
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
		case r.Method == http.MethodGet && r.URL.Path == "/v1/contacts":
			_, _ = w.Write([]byte(`{"data":[{"id":"0199152d-525d-7000-8000-000000000001"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/series":
			_, _ = w.Write([]byte(`{"data":[{"id":"ser_1","is_default":false},{"id":"ser_2","is_default":true}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/taxes/active":
			_, _ = w.Write([]byte(`{"data":[{"id":"tax_1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/invoices":
			_, _ = w.Write([]byte(`{"data":{"id":"inv_1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/invoices/inv_1/mark-sent":
			_, _ = w.Write([]byte(`{"data":{"id":"inv_1","status":"sent"}}`))
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
	sentIdx := indexOf(calls, "POST /v1/invoices/inv_1/mark-sent")
	if sentIdx < 0 {
		t.Fatalf("falta POST /v1/invoices/inv_1/mark-sent; calls=%v", calls)
	}
	payIdx := indexOf(calls, "POST /v1/invoices/inv_1/mark-paid")
	if payIdx < 0 {
		t.Fatalf("falta POST /v1/invoices/inv_1/mark-paid; calls=%v", calls)
	}
	if !(createIdx < sentIdx && sentIdx < payIdx) {
		t.Fatalf("orden esperado crear < mark-sent < mark-paid; calls=%v", calls)
	}
	for _, dep := range []string{"GET /v1/contacts", "GET /v1/series", "GET /v1/taxes/active"} {
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
		"project.created",
		"quote.approved",
		"quote.created",
		"task.created",
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
