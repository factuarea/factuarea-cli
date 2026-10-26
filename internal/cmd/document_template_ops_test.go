package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Operaciones reales de plantillas PDF y presentación, tomadas de la tabla
// generada (no sintéticas), ejecutadas contra un servidor de pruebas. Los CE que
// dependen de reglas del servidor (CE01-CE04, CE06, CE07, CE09-CE13, CE18-CE22,
// CE24) quedan fuera: el CLI solo transporta cuerpo, cabeceras y bytes.

// runWithoutScopeCheck ejecuta la operación sin la consulta previa de scopes, que
// el servidor de pruebas no implementa y que contaría como una llamada más.
func runWithoutScopeCheck(t *testing.T, op genOp, baseURL string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	return runOp(t, op, baseURL, append(args, "--skip-scope-check")...)
}

func realOp(t *testing.T, id string) genOp {
	t.Helper()
	for _, op := range generatedOps() {
		if op.OperationID == id {
			return op
		}
	}
	t.Fatalf("la tabla generada no contiene %s", id)
	return genOp{}
}

type seenRequest struct {
	method, path, query, ifMatch, idem, ct string
	body                                   []byte
}

func recordingServer(t *testing.T, respond func(w http.ResponseWriter, n int)) (*httptest.Server, func() []seenRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []seenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, seenRequest{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("If-Match"), r.Header.Get("Idempotency-Key"), r.Header.Get("Content-Type"), b})
		n := len(seen)
		mu.Unlock()
		respond(w, n)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []seenRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]seenRequest(nil), seen...)
	}
}

func okJSON(w http.ResponseWriter, _ int) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
}

func jsonEqual(t *testing.T, got []byte, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("cuerpo no JSON %q: %v", got, err)
	}
	_ = json.Unmarshal([]byte(want), &w)
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if !bytes.Equal(gb, wb) {
		t.Errorf("cuerpo = %s, quiero %s", got, want)
	}
}

// CE05, CE25: Series.update es un PATCH generado con su cuerpo y null explícito,
// dirigido a la serie dentro de la empresa, y toda escritura lleva Idempotency-Key.
func TestSeriesUpdateIsGeneratedPATCHWithExplicitNull(t *testing.T) {
	srv, seen := recordingServer(t, okJSON)
	_, _, err := runWithoutScopeCheck(t, realOp(t, "public-api.v1.series.update"), srv.URL, "acme_co", "ser1", "--name", "Serie A", "--null-pdf-template-id", "--json")
	if err != nil {
		t.Fatal(err)
	}
	got := seen()
	if len(got) != 1 || got[0].method != "PATCH" || got[0].path != "/v1/companies/acme_co/series/ser1" || got[0].idem == "" {
		t.Fatalf("petición = %+v", got)
	}
	jsonEqual(t, got[0].body, `{"name":"Serie A","pdf_template_id":null}`)
}

// CE03, CE06, CE09, CE25: campo requerido y nullable (template_id) con If-Match
// literal; omitirlo es un error de uso, null va en el cuerpo.
func TestAssignPresentationRequiredNullableAndIfMatch(t *testing.T) {
	op := realOp(t, "public-api.v1.invoices.assign_presentation")
	srv, seen := recordingServer(t, okJSON)

	if _, _, err := runWithoutScopeCheck(t, op, srv.URL, "acme_co", "inv1", "--expected-version", "3"); err == nil {
		t.Fatal("omitir template_id (requerido) debe ser un error de uso")
	}
	if len(seen()) != 0 {
		t.Fatal("no debe llamar a la API con un requerido ausente")
	}
	if _, _, err := runWithoutScopeCheck(t, op, srv.URL, "acme_co", "inv1", "--null-template-id", "--expected-version", "3", "--if-match", `"p1"`, "--json"); err != nil {
		t.Fatal(err)
	}
	got := seen()
	if len(got) != 1 || got[0].method != "PATCH" || got[0].path != "/v1/companies/acme_co/invoices/inv1/presentation" || got[0].ifMatch != `"p1"` {
		t.Fatalf("petición = %+v", got)
	}
	jsonEqual(t, got[0].body, `{"template_id":null,"expected_version":"3"}`)
}

// CE05, CE25: presencia en un PATCH real con --data-file: null, vacío y ausente
// no se confunden.
func TestTemplateUpdateDataFileKeepsNullAndEmpty(t *testing.T) {
	file := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(file, []byte(`{"description":null,"name":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, seen := recordingServer(t, okJSON)
	if _, _, err := runWithoutScopeCheck(t, realOp(t, "public-api.v1.pdf_templates.update"), srv.URL, "acme_co", "tpl1", "--data-file", file, "--if-match", `"t9"`, "--json"); err != nil {
		t.Fatal(err)
	}
	got := seen()
	if len(got) != 1 || got[0].method != "PATCH" || got[0].path != "/v1/companies/acme_co/pdf-templates/tpl1" || got[0].ifMatch != `"t9"` {
		t.Fatalf("petición = %+v", got)
	}
	jsonEqual(t, got[0].body, `{"description":null,"name":""}`)
}

// CE08: el activo se descarga como imagen (bytes exactos a -o) o como JSON según
// el Content-Type de la respuesta.
func TestAssetGetDualResponseFollowsContentType(t *testing.T) {
	op := realOp(t, "public-api.v1.pdf_templates.asset_get")
	if !op.DualBinary {
		t.Fatal("asset_get debe estar marcada como descarga dual")
	}
	image := []byte("\x89PNG\r\n\x1a\n\x00\xff")
	srv, _ := recordingServer(t, func(w http.ResponseWriter, n int) {
		if n == 1 {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(image)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"data":{"id":"a1","kind":"logo"}}`))
	})
	out := filepath.Join(t.TempDir(), "logo.png")
	stdout, _, err := runWithoutScopeCheck(t, op, srv.URL, "acme_co", "a1", "-o", out)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); !bytes.Equal(got, image) || stdout != "" {
		t.Errorf("imagen: fichero=%q stdout=%q", got, stdout)
	}
	out2 := filepath.Join(t.TempDir(), "meta.json")
	stdout, _, err = runWithoutScopeCheck(t, op, srv.URL, "acme_co", "a1", "-o", out2, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"kind":"logo"`) {
		t.Errorf("JSON: stdout = %q", stdout)
	}
	if _, statErr := os.Stat(out2); !os.IsNotExist(statErr) {
		t.Error("una respuesta JSON no debe escribirse en -o como si fuera una imagen")
	}
}

// CE08: la subida real de un activo viaja como multipart con kind y fichero.
func TestAssetUploadRealOperationSendsMultipart(t *testing.T) {
	logo := []byte("\x89PNG\r\n\x1a\n\x00logo")
	file := filepath.Join(t.TempDir(), "logo.png")
	if err := os.WriteFile(file, logo, 0o600); err != nil {
		t.Fatal(err)
	}
	srv, seen := recordingServer(t, okJSON)
	if _, _, err := runWithoutScopeCheck(t, realOp(t, "public-api.v1.pdf_templates.asset_upload"), srv.URL, "--company", "acme_co", "--file-file", file, "--data", `{"kind":"logo"}`, "--json"); err != nil {
		t.Fatal(err)
	}
	got := seen()
	if len(got) != 1 || got[0].method != "POST" || got[0].path != "/v1/companies/acme_co/pdf-templates/assets" || got[0].idem == "" {
		t.Fatalf("petición = %+v", got)
	}
	if !strings.HasPrefix(got[0].ct, "multipart/form-data; boundary=") || !bytes.Contains(got[0].body, logo) || !bytes.Contains(got[0].body, []byte("logo")) {
		t.Errorf("multipart incorrecto: %q", got[0].ct)
	}
}

// CE16, CE17, CE25: descarga real de un artefacto PDF: 200 bytes, 202 JSON sin
// tocar -o y 304 sin salida.
func TestArtifactDownloadStatuses(t *testing.T) {
	op := realOp(t, "public-api.v1.pdf_artifacts.download")
	pdf := []byte("%PDF-1.7\x00\xff")
	srv, seen := recordingServer(t, func(w http.ResponseWriter, n int) {
		switch n {
		case 1:
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("ETag", `"a1"`)
			_, _ = w.Write(pdf)
		case 2:
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"data":{"state":"processing"}}`))
		default:
			w.Header().Set("ETag", `"a1"`)
			w.WriteHeader(http.StatusNotModified)
		}
	})
	dir := t.TempDir()
	ok, pending, same := filepath.Join(dir, "ok.pdf"), filepath.Join(dir, "pending.pdf"), filepath.Join(dir, "same.pdf")

	if _, stderr, err := runWithoutScopeCheck(t, op, srv.URL, "acme_co", "art1", "-o", ok, "--show-etag"); err != nil || !strings.Contains(stderr, `etag: "a1"`) {
		t.Fatalf("200: err=%v stderr=%q", err, stderr)
	}
	if got, _ := os.ReadFile(ok); !bytes.Equal(got, pdf) {
		t.Errorf("bytes del 200 = %q", got)
	}
	stdout, stderr, err := runWithoutScopeCheck(t, op, srv.URL, "acme_co", "art1", "-o", pending, "--json")
	if err != nil || !strings.Contains(stdout, "processing") || !strings.Contains(stderr, "retry-after: 2") {
		t.Fatalf("202: err=%v stdout=%q stderr=%q", err, stdout, stderr)
	}
	if _, statErr := os.Stat(pending); !os.IsNotExist(statErr) {
		t.Error("un 202 no debe crear el PDF")
	}
	stdout, _, err = runWithoutScopeCheck(t, op, srv.URL, "acme_co", "art1", "-o", same, "--if-none-match", `"a1"`)
	if err != nil || stdout != "" {
		t.Fatalf("304: err=%v stdout=%q", err, stdout)
	}
	if _, statErr := os.Stat(same); !os.IsNotExist(statErr) {
		t.Error("un 304 no debe crear el PDF")
	}
	if len(seen()) != 3 {
		t.Errorf("llamadas = %d, quiero 3 (una por ejecución)", len(seen()))
	}
}

// CE15: el envío a FACe real hace un solo intento ante un 502.
func TestFaceSubmitRealOperationIsSingleAttempt(t *testing.T) {
	srv, seen := recordingServer(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"type":"api_error","code":"face_transmission_uncertain"}}`))
	})
	_, _, err := runWithoutScopeCheck(t, realOp(t, "public-api.v1.invoices.face_submissions.submit"), srv.URL, "acme_co", "inv1", "--confirm", "inv1", "--no-input")
	if err == nil {
		t.Fatal("esperaba el 502 como error")
	}
	if n := len(seen()); n != 1 {
		t.Errorf("FACe: %d llamadas, quiero 1", n)
	}
}

// CE06, CE25: un listado por offset (page/per_page) es una petición y no ofrece
// recorrido por cursor.
func TestOffsetListingIsASingleRequestWithoutPaginateFlag(t *testing.T) {
	op := realOp(t, "public-api.v1.invoices.list_presentations")
	srv, seen := recordingServer(t, okJSON)
	if _, _, err := runWithoutScopeCheck(t, op, srv.URL, "acme_co", "inv1", "--page", "2", "--per_page", "5", "--json"); err != nil {
		t.Fatal(err)
	}
	got := seen()
	if len(got) != 1 || got[0].path != "/v1/companies/acme_co/invoices/inv1/presentation-history" || got[0].query != "page=2&per_page=5" {
		t.Fatalf("petición = %+v", got)
	}
	if _, _, err := runWithoutScopeCheck(t, op, srv.URL, "acme_co", "inv1", "--paginate"); err == nil {
		t.Error("un listado offset no ofrece --paginate")
	}
}
