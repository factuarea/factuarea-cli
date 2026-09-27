package client

import (
	"context"
	"mime"
	"mime/multipart"
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

// TestMultipartBodyRepeatedFileField cubre el caso del escáner de compras
// (`purchase_scans.create`): un array de ficheros se envía como varias partes
// bajo el MISMO nombre de campo (`files[]`), una por fichero, en el orden
// recibido (`design.md` D5).
func TestMultipartBodyRepeatedFileField(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.pdf")
	b := filepath.Join(dir, "b.png")
	_ = os.WriteFile(a, []byte("PDF-BYTES"), 0o600)
	_ = os.WriteFile(b, []byte("PNG-BYTES"), 0o600)

	body, ct, err := MultipartBody(nil, nil, map[string][]string{"files[]": {a, b}})
	if err != nil {
		t.Fatal(err)
	}

	_, params, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatalf("Content-Type inválido: %v", err)
	}
	mr := multipart.NewReader(strings.NewReader(string(body)), params["boundary"])

	var names, filenames []string
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		names = append(names, part.FormName())
		filenames = append(filenames, part.FileName())
	}

	if len(names) != 2 {
		t.Fatalf("esperaba 2 partes, got %d (%v)", len(names), names)
	}
	for _, n := range names {
		if n != "files[]" {
			t.Errorf("nombre de campo = %q, want files[] en las 2 partes: %v", n, names)
		}
	}
	if filenames[0] != "a.pdf" || filenames[1] != "b.png" {
		t.Errorf("filenames = %v, want [a.pdf b.png] (orden recibido)", filenames)
	}
}

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
