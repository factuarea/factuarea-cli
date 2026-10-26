package cmd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
)

// runOp monta `op` bajo el root real y lo ejecuta contra baseURL con stdout y
// stderr separados (los de runCmd van mezclados).
func runOp(t *testing.T, op genOp, baseURL string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", baseURL)
	op.Action = "run"
	root := NewRootCmd()
	grp := &cobra.Command{Use: "t"}
	grp.AddCommand(buildGeneratedCommand(op))
	root.AddCommand(grp)
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(append([]string{"t", "run"}, args...))
	err = root.Execute()
	return out.String(), errb.String(), err
}

var presenceBody = &genBody{Kind: "json", Fields: []genBodyField{
	{Name: "pdf_template_id", Type: "string", Kind: "scalar", Nullable: true},
	{Name: "expected_version", Type: "integer", Kind: "scalar"},
	{Name: "name", Type: "string", Kind: "scalar"},
	{Name: "active", Type: "boolean", Kind: "scalar"},
	{Name: "tags", Type: "string", Kind: "scalar_array", Nullable: true},
}}

func patchOp() genOp {
	return genOp{
		OperationID: "public-api.v1.series.assign_pdf_template", Method: "PATCH", Path: "/companies/{company}/series/{series}/pdf-template",
		PathParams:   []genParam{{Name: "company", In: "path", Type: "string", Required: true}, {Name: "series", In: "path", Type: "string", Required: true}},
		HeaderParams: []genParam{{Name: "If-Match", In: "header", Type: "string"}},
		ReturnsETag:  true,
		Body:         presenceBody,
	}
}

func jsonServer(t *testing.T, onRequest func(r *http.Request, body string)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if onRequest != nil {
			onRequest(r, string(b))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `"3fa9"`)
		_, _ = w.Write([]byte(`{"data":{"id":"x"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestIfMatchIsSentLiteralAndOnlyOnOpsThatDeclareIt(t *testing.T) {
	var got string
	var seen bool
	srv := jsonServer(t, func(r *http.Request, _ string) {
		got, seen = r.Header.Get("If-Match"), true
	})
	if _, _, err := runOp(t, patchOp(), srv.URL, "acme_co", "s1", "--if-match", `"abc"`, "--expected-version", "2"); err != nil {
		t.Fatal(err)
	}
	if !seen || got != `"abc"` {
		t.Fatalf("If-Match = %q, quiero literal \"abc\" con comillas", got)
	}

	got = "no-llamado"
	op := patchOp()
	op.HeaderParams = nil
	if _, _, err := runOp(t, op, srv.URL, "acme_co", "s1", "--if-match", "x"); err == nil || !strings.Contains(err.Error(), "if-match") {
		t.Fatalf("sin declararlo, --if-match no existe: %v", err)
	}
	if got != "no-llamado" {
		t.Error("no debía llegar a la red")
	}
	if _, _, err := runOp(t, patchOp(), srv.URL, "acme_co", "s1", "--if-match", "", "--expected-version", "2"); err == nil || !strings.Contains(err.Error(), "--if-match no puede estar vacío") {
		t.Fatalf("un If-Match vacío es un error de uso: %v", err)
	}
}

func TestRequiredHeaderFlagIsEnforced(t *testing.T) {
	op := patchOp()
	op.HeaderParams = []genParam{{Name: "If-Match", In: "header", Type: "string", Required: true}}
	srv := jsonServer(t, func(*http.Request, string) { t.Error("sin la cabecera requerida no se llama") })
	if _, _, err := runOp(t, op, srv.URL, "acme_co", "s1", "--expected-version", "2"); err == nil || !strings.Contains(err.Error(), "--if-match") {
		t.Fatalf("error = %v", err)
	}
}

func TestShowETagGoesToStderrAndKeepsStdoutClean(t *testing.T) {
	srv := jsonServer(t, nil)
	stdout, stderr, err := runOp(t, patchOp(), srv.URL, "acme_co", "s1", "--expected-version", "2", "--show-etag", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, `etag: "3fa9"`) {
		t.Errorf("stderr = %q", stderr)
	}
	if strings.Contains(stdout, "3fa9") || !strings.Contains(stdout, `"id":"x"`) {
		t.Errorf("stdout debe ser solo el cuerpo: %q", stdout)
	}

	_, stderr, _ = runOp(t, patchOp(), srv.URL, "acme_co", "s1", "--expected-version", "2", "--json")
	if strings.Contains(stderr, "etag") {
		t.Errorf("sin --show-etag no se escribe nada: %q", stderr)
	}

	op := patchOp()
	op.ReturnsETag = false
	if _, _, err := runOp(t, op, srv.URL, "acme_co", "s1", "--show-etag"); err == nil {
		t.Error("--show-etag solo existe si la respuesta declara ETag")
	}
}

func TestBodyDistinguishesOmittedNullAndEmpty(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"omitido", []string{"--expected-version", "2"}, `{"expected_version":2}`},
		{"nada", nil, `{}`},
		{"null explícito", []string{"--null-pdf-template-id", "--expected-version", "2"}, `{"expected_version":2,"pdf_template_id":null}`},
		{"vacíos", []string{"--name", "", "--active=false", "--expected-version", "0", "--tags="}, `{"active":false,"expected_version":0,"name":"","tags":[]}`},
		{"null en lista", []string{"--null-tags"}, `{"tags":null}`},
		{"uuid", []string{"--pdf-template-id", "019e0000"}, `{"pdf_template_id":"019e0000"}`},
		{"raw", []string{"--data", `{"pdf_template_id":null,"tags":[],"active":false,"expected_version":0,"name":""}`}, `{"active":false,"expected_version":0,"name":"","pdf_template_id":null,"tags":[]}`},
		{"raw vacío", []string{"--data", `{}`}, `{}`},
		{"raw sin tocar números", []string{"--data", `{"expected_version":9007199254740993}`}, `{"expected_version":9007199254740993}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body string
			srv := jsonServer(t, func(_ *http.Request, b string) { body = b })
			args := append([]string{"acme_co", "s1", "--json"}, tc.args...)
			if _, _, err := runOp(t, patchOp(), srv.URL, args...); err != nil {
				t.Fatal(err)
			}
			if body != tc.want {
				t.Errorf("cuerpo = %s, quiero %s", body, tc.want)
			}
		})
	}
}

func TestBodyFromDataFileKeepsNullAndEmpty(t *testing.T) {
	var body string
	srv := jsonServer(t, func(_ *http.Request, b string) { body = b })
	f := filepath.Join(t.TempDir(), "b.json")
	if err := os.WriteFile(f, []byte(`{"pdf_template_id": null, "tags": [], "active": false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runOp(t, patchOp(), srv.URL, "acme_co", "s1", "--data-file", f); err != nil {
		t.Fatal(err)
	}
	if body != `{"active":false,"pdf_template_id":null,"tags":[]}` {
		t.Errorf("cuerpo = %s", body)
	}
}

func TestNullFlagConflictsAreUsageErrors(t *testing.T) {
	srv := jsonServer(t, func(*http.Request, string) { t.Error("no debe llamar") })
	if _, _, err := runOp(t, patchOp(), srv.URL, "acme_co", "s1", "--pdf-template-id", "x", "--null-pdf-template-id"); err == nil || !strings.Contains(err.Error(), "--null-pdf-template-id") {
		t.Errorf("valor y null a la vez: %v", err)
	}
	if _, _, err := runOp(t, patchOp(), srv.URL, "acme_co", "s1", "--null-pdf-template-id", "--data", `{}`); err == nil || !strings.Contains(err.Error(), "no mezcles") {
		t.Errorf("--null-* con --data: %v", err)
	}
	op := patchOp()
	if _, _, err := runOp(t, op, srv.URL, "acme_co", "s1", "--null-name"); err == nil {
		t.Error("name no es nullable: no hay --null-name")
	}
}

func pdfOp() genOp {
	return genOp{
		OperationID: "public-api.v1.invoices.pdf", Method: "GET", Path: "/companies/{company}/invoices/{invoice}/pdf",
		PathParams:        []genParam{{Name: "company", In: "path", Type: "string", Required: true}, {Name: "invoice", In: "path", Type: "string", Required: true}},
		HeaderParams:      []genParam{{Name: "If-None-Match", In: "header", Type: "string"}},
		ReturnsETag:       true,
		BinaryContentType: "application/pdf",
	}
}

func TestBinaryDownloadWritesExactBytes(t *testing.T) {
	pdf := []byte("%PDF-1.7\r\n\x00\xff\xfe\n%%EOF")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("ETag", `"pdf1"`)
		_, _ = w.Write(pdf)
	}))
	t.Cleanup(srv.Close)
	out := filepath.Join(t.TempDir(), "f.pdf")
	stdout, stderr, err := runOp(t, pdfOp(), srv.URL, "acme_co", "inv1", "-o", out, "--show-etag")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	if !bytes.Equal(got, pdf) {
		t.Errorf("bytes distintos: %q", got)
	}
	if stdout != "" || !strings.Contains(stderr, `etag: "pdf1"`) {
		t.Errorf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestBinaryOperationAccepted202IsPlainJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"data":{"state":"queued"}}`))
	}))
	t.Cleanup(srv.Close)
	out := filepath.Join(t.TempDir(), "f.pdf")
	stdout, stderr, err := runOp(t, pdfOp(), srv.URL, "acme_co", "inv1", "-o", out, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"state":"queued"`) {
		t.Errorf("el 202 se imprime como JSON: %q", stdout)
	}
	if !strings.Contains(stderr, "retry-after: 2") {
		t.Errorf("stderr = %q", stderr)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Error("un 202 no debe crear el fichero de salida")
	}
}

func TestNotModifiedKeepsOutputFileAndSendsIfNoneMatch(t *testing.T) {
	var inm string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inm = r.Header.Get("If-None-Match")
		w.Header().Set("ETag", inm)
		w.WriteHeader(http.StatusNotModified)
	}))
	t.Cleanup(srv.Close)
	out := filepath.Join(t.TempDir(), "f.pdf")
	if err := os.WriteFile(out, []byte("VIEJO"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runOp(t, pdfOp(), srv.URL, "acme_co", "inv1", "-o", out, "--if-none-match", `"pdf1"`, "--show-etag")
	if err != nil {
		t.Fatalf("304 es éxito: %v", err)
	}
	if inm != `"pdf1"` {
		t.Errorf("If-None-Match = %q", inm)
	}
	if got, _ := os.ReadFile(out); string(got) != "VIEJO" {
		t.Errorf("el 304 no debe tocar el fichero: %q", got)
	}
	if stdout != "" || !strings.Contains(stderr, `etag: "pdf1"`) {
		t.Errorf("stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestServiceUnavailableReportsRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	out := filepath.Join(t.TempDir(), "f.pdf")
	if err := os.WriteFile(out, []byte("VIEJO"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runOp(t, pdfOp(), srv.URL, "acme_co", "inv1", "-o", out)
	if err == nil {
		t.Fatal("esperaba error")
	}
	if !strings.Contains(stderr, `"retry_after":"0"`) || !strings.Contains(stderr, "503") {
		t.Errorf("stderr = %q", stderr)
	}
	if got, _ := os.ReadFile(out); string(got) != "VIEJO" {
		t.Errorf("un error no debe truncar el fichero: %q", got)
	}
}

func TestNoRetryOperationMakesASingleCall(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	op := genOp{
		OperationID: "public-api.v1.invoices.face_submissions.submit", Method: "POST", Path: "/companies/{company}/invoices/{invoice}/face-submissions",
		PathParams: []genParam{{Name: "company", In: "path", Type: "string", Required: true}, {Name: "invoice", In: "path", Type: "string", Required: true}},
		NoRetry:    true,
	}
	_, stderr, err := runOp(t, op, srv.URL, "acme_co", "inv1")
	if err == nil {
		t.Fatal("esperaba error")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("noRetry: %d llamadas, quiero 1", n)
	}
	if !strings.Contains(stderr, `"retry_after":"30"`) {
		t.Errorf("stderr = %q", stderr)
	}

	calls.Store(0)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv2.Close)
	op.NoRetry = false
	if _, _, err := runOp(t, op, srv2.URL, "acme_co", "inv1"); err == nil {
		t.Fatal("esperaba error")
	}
	if n := calls.Load(); n != 4 {
		t.Fatalf("operación normal: %d llamadas, quiero 4 (1 + 3 reintentos)", n)
	}
}

func TestMultipartAssetUploadSendsFileAndKind(t *testing.T) {
	logo := []byte("\x89PNG\r\n\x1a\n\x00binary")
	f := filepath.Join(t.TempDir(), "logo.png")
	if err := os.WriteFile(f, logo, 0o600); err != nil {
		t.Fatal(err)
	}
	var gotKind, gotName, ct, idem string
	var gotFile []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct, idem = r.Header.Get("Content-Type"), r.Header.Get("Idempotency-Key")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart: %v", err)
		}
		gotKind = r.FormValue("kind")
		file, hdr, err := r.FormFile("file")
		if err != nil {
			t.Errorf("falta la parte file: %v", err)
		} else {
			gotName = hdr.Filename
			gotFile, _ = io.ReadAll(file)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"a1"}}`))
	}))
	t.Cleanup(srv.Close)
	op := genOp{
		OperationID: "public-api.v1.pdf_templates.asset_upload", Method: "POST", Path: "/companies/{company}/pdf-templates/assets",
		PathParams:          []genParam{{Name: "company", In: "path", Type: "string", Required: true}},
		IdempotencyRequired: true,
		Body:                &genBody{Kind: "multipart", FileFields: []string{"file"}},
	}
	stdout, _, err := runOp(t, op, srv.URL, "--company", "acme_co", "--file-file", f, "--data", `{"kind":"logo"}`, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if gotKind != "logo" || gotName != "logo.png" || !bytes.Equal(gotFile, logo) {
		t.Errorf("kind=%q name=%q file=%q", gotKind, gotName, gotFile)
	}
	if !strings.HasPrefix(ct, "multipart/form-data; boundary=") || idem == "" {
		t.Errorf("content-type=%q idempotency=%q", ct, idem)
	}
	if !strings.Contains(stdout, `"id":"a1"`) {
		t.Errorf("stdout = %q", stdout)
	}
}
