package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const scannerTestID = "0199152d-525d-7000-8000-000000000011"

func TestScannerUploadSendsAllOriginals(t *testing.T) {
	first := filepath.Join(t.TempDir(), "invoice, one.pdf")
	second := filepath.Join(t.TempDir(), "receipt.png")
	if err := os.WriteFile(first, []byte("%PDF-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("PNG-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/purchase_scans" || r.Method != "POST" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "scanner-batch-key-001" {
			t.Errorf("key = %q", got)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		defer r.MultipartForm.RemoveAll()
		files := r.MultipartForm.File["files[]"]
		if len(files) != 2 {
			t.Fatalf("got %d files", len(files))
		}
		if files[0].Filename != "invoice, one.pdf" || files[1].Filename != "receipt.png" {
			t.Errorf("filenames: %+v", files)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		_, _ = w.Write([]byte(`{"data":{"accepted":[{"id":"` + scannerTestID + `","item_index":0}],"rejected":[],"idempotent_replay":false}}`))
	}))
	defer srv.Close()
	out, err := runCmd(t, srv.URL, "purchase-scans", "create", "--file-files", first, "--file-files", second, "--idempotency-key", "scanner-batch-key-001", "--skip-scope-check", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(out), []byte(scannerTestID)) {
		t.Fatalf("missing accepted scan: %s", out)
	}
}

func TestScannerArchiveSendsVersionAndIdempotency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.RawQuery != "" {
			t.Errorf("request %s %s", r.Method, r.URL)
		}
		if len(r.Header.Get("Idempotency-Key")) < 16 {
			t.Error("missing stable idempotency key")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["expected_version"] != float64(4) {
			t.Errorf("body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"status":"archived"}}`))
	}))
	defer srv.Close()
	if _, err := runCmd(t, srv.URL, "purchase-scans", "archive", scannerTestID, "--expected-version", "4", "--skip-scope-check"); err != nil {
		t.Fatal(err)
	}
}

func TestScannerSourceDownloadsOriginalBytes(t *testing.T) {
	want := []byte("original-image")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/purchase_scans/"+scannerTestID+"/source" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(want)
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "source.png")
	if _, err := runCmd(t, srv.URL, "purchase-scans", "source", scannerTestID, "-o", path, "--skip-scope-check"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("original was modified: %q", got)
	}
}
