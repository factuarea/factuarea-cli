package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

func TestResolveFormat(t *testing.T) {
	if f := ResolveFormat(true, true); f != JSON {
		t.Fatal("explicit --json must win over TTY")
	}
	if f := ResolveFormat(false, true); f != Human {
		t.Fatal("TTY default is Human")
	}
	if f := ResolveFormat(false, false); f != JSON {
		t.Fatal("non-TTY default is JSON")
	}
}

func TestPrintErrorJSON(t *testing.T) {
	var buf bytes.Buffer
	PrintError(&buf, &apierr.APIError{Type: "not_found_error", Code: "invoice_not_found", Message: "No existe", RequestID: "req_1"}, JSON)
	s := buf.String()
	if !strings.Contains(s, `"type":"not_found_error"`) || !strings.Contains(s, `"request_id":"req_1"`) {
		t.Fatalf("bad json error: %s", s)
	}
}

func TestPrintErrorHuman(t *testing.T) {
	var buf bytes.Buffer
	PrintError(&buf, &apierr.APIError{Code: "invoice_not_found", Message: "No existe la factura", DocURL: "https://docs.factuarea.com/x"}, Human)
	s := buf.String()
	if !strings.Contains(s, "No existe la factura") || !strings.Contains(s, "docs.factuarea.com") {
		t.Fatalf("bad human error: %s", s)
	}
}
