package apierr

import (
	"errors"
	"testing"
)

func TestAPIErrorWithCode(t *testing.T) {
	e := &APIError{Message: "factura no encontrada", Code: "invoice_not_found"}
	got := e.Error()
	want := "factura no encontrada (invoice_not_found)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAPIErrorWithoutCode(t *testing.T) {
	e := &APIError{Message: "algo salió mal"}
	if got := e.Error(); got != "algo salió mal" {
		t.Fatalf("got %q, want %q", got, "algo salió mal")
	}
}

func TestTransportErrorUnwrap(t *testing.T) {
	inner := errors.New("dial tcp: timeout")
	e := &TransportError{Err: inner}
	if got := e.Error(); got != "dial tcp: timeout" {
		t.Fatalf("got %q, want %q", got, "dial tcp: timeout")
	}
	if e.Unwrap() != inner {
		t.Fatalf("Unwrap did not return the inner error")
	}
	if !errors.Is(e, inner) {
		t.Fatalf("errors.Is must traverse Unwrap to the inner error")
	}
}

func TestUsageErrorUnwrap(t *testing.T) {
	inner := errors.New("falta --live")
	e := &UsageError{Err: inner}
	if got := e.Error(); got != "falta --live" {
		t.Fatalf("got %q, want %q", got, "falta --live")
	}
	if e.Unwrap() != inner {
		t.Fatalf("Unwrap did not return the inner error")
	}
	if !errors.Is(e, inner) {
		t.Fatalf("errors.Is must traverse Unwrap to the inner error")
	}
}
