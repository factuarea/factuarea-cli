package client

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultipartBody(t *testing.T) {
	f := filepath.Join(t.TempDir(), "cert.p12")
	_ = os.WriteFile(f, []byte("BINARY"), 0o600)
	body, ct, err := MultipartBody(map[string]string{"certificate_password": "x"}, map[string]string{"certificate_file": f})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ct, "multipart/form-data; boundary=") {
		t.Fatalf("content-type: %q", ct)
	}
	if !strings.Contains(string(body), "certificate_password") || !strings.Contains(string(body), "BINARY") {
		t.Fatal("multipart no contiene campo/archivo")
	}
	// El nombre del fichero subido debe ser el basename, no la ruta completa.
	if !strings.Contains(string(body), `filename="cert.p12"`) {
		t.Fatalf("filename no es el basename: %s", body)
	}
}

func TestMultipartBodyMissingFileFails(t *testing.T) {
	_, _, err := MultipartBody(nil, map[string]string{"certificate_file": "/no/existe/cert.p12"})
	if err == nil {
		t.Fatal("esperaba error al abrir fichero inexistente")
	}
}

// TestDoRespectsExplicitContentType comprueba que un Content-Type explícito en
// extraHeaders (multipart) NO se pisa por el application/json por defecto.
func TestDoRespectsExplicitContentType(t *testing.T) {
	const wantCT = "multipart/form-data; boundary=abc123"
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != wantCT {
			t.Errorf("Content-Type pisado: got %q want %q", got, wantCT)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	})
	_, err := c.Do(context.Background(), http.MethodPost, "/v1/company/certificates", []byte("BINARY"), map[string]string{
		"Content-Type": wantCT,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
}

// TestDoRespectsExplicitIdempotencyKey comprueba que una Idempotency-Key
// explícita en extraHeaders gana sobre la autogenerada.
func TestDoRespectsExplicitIdempotencyKey(t *testing.T) {
	const wantKey = "my-explicit-key"
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Idempotency-Key"); got != wantKey {
			t.Errorf("Idempotency-Key: got %q want %q", got, wantKey)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	})
	_, err := c.Do(context.Background(), http.MethodPost, "/v1/invoices", []byte(`{}`), map[string]string{
		"Idempotency-Key": wantKey,
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
}
