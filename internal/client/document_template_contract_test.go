package client

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

// Transporte de las operaciones de plantillas PDF y documentos: bytes exactos,
// 202/304, Retry-After y operaciones sin reintento, contra HTTP real (httptest).
// Las fixtures son sintéticas y locales a este fichero.

type recordedCall struct {
	method, ct, idem, ifMatch string
	body                      []byte
}

// scriptedServer responde con steps[i] a la i-ésima llamada (la última se repite)
// y registra cada petición recibida.
func scriptedServer(t *testing.T, steps ...func(w http.ResponseWriter)) (*httptest.Server, func() []recordedCall) {
	t.Helper()
	var mu sync.Mutex
	var calls []recordedCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, recordedCall{r.Method, r.Header.Get("Content-Type"), r.Header.Get("Idempotency-Key"), r.Header.Get("If-Match"), b})
		i := len(calls) - 1
		mu.Unlock()
		if i >= len(steps) {
			i = len(steps) - 1
		}
		steps[i](w)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []recordedCall {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedCall(nil), calls...)
	}
}

func reply(status int, contentType string, body []byte, headers ...string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		for i := 0; i+1 < len(headers); i += 2 {
			w.Header().Set(headers[i], headers[i+1])
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}
}

func noSleepClient(srv *httptest.Server, slept *[]time.Duration) *Client {
	return New("fact_test_x", WithBaseURL(srv.URL), WithSleep(func(d time.Duration) {
		if slept != nil {
			*slept = append(*slept, d)
		}
	}))
}

func syntheticZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("factura.pdf")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("%PDF-1.7\x00\xff"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// CE08, CE14, CE16, CE23: el cuerpo de una descarga llega byte a byte (digest) con
// su media type y su ETag, sea PDF, XML, imagen o ZIP.
func TestBinaryBodiesKeepExactBytesAndHeaders(t *testing.T) {
	cases := []struct {
		name, contentType string
		body              []byte
	}{
		{"pdf", "application/pdf", []byte("%PDF-1.7\r\n\x00\xff\xfe\n%%EOF")},
		{"xml", "application/xml", []byte("\xef\xbb\xbf<?xml version=\"1.0\"?>\r\n<fe:Facturae>\xc3\xb1</fe:Facturae>\n")},
		{"imagen", "image/png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\xff")},
		{"zip", "application/zip", syntheticZip(t)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := scriptedServer(t, reply(200, tc.contentType, tc.body, "ETag", `"`+tc.name+`"`))
			resp, err := noSleepClient(srv, nil).Do(context.Background(), "GET", "/v1/x", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if sha256.Sum256(resp.Body) != sha256.Sum256(tc.body) {
				t.Errorf("digest distinto: %d bytes recibidos, %d enviados", len(resp.Body), len(tc.body))
			}
			if resp.ContentType != tc.contentType || resp.Header.Get("ETag") != `"`+tc.name+`"` {
				t.Errorf("content-type=%q etag=%q", resp.ContentType, resp.Header.Get("ETag"))
			}
		})
	}
}

// CE16, CE17: un 202 es éxito JSON (no bytes), conserva Retry-After y no se
// reintenta; el sondeo posterior devuelve el 200.
func TestAcceptedThenReadyPollingIsOneCallEach(t *testing.T) {
	pdf := []byte("%PDF-1.7 listo")
	srv, calls := scriptedServer(t,
		reply(202, "application/json", []byte(`{"data":{"state":"processing"}}`), "Retry-After", "3"),
		reply(200, "application/pdf", pdf),
	)
	var slept []time.Duration
	c := noSleepClient(srv, &slept)

	first, err := c.Do(context.Background(), "GET", "/v1/companies/acme_co/pdf-artifacts/a/download", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.StatusCode != 202 || first.Header.Get("Retry-After") != "3" || first.ContentType != "application/json" {
		t.Fatalf("202 = %+v", first)
	}
	second, err := c.Do(context.Background(), "GET", "/v1/companies/acme_co/pdf-artifacts/a/download", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.StatusCode != 200 || !bytes.Equal(second.Body, pdf) {
		t.Fatalf("200 = %+v", second)
	}
	if len(calls()) != 2 || len(slept) != 0 {
		t.Errorf("llamadas=%d esperas=%v: un 202 no se reintenta solo", len(calls()), slept)
	}
}

// CE25: 304 sin cuerpo es éxito y la petición lleva el validador literal.
func TestConditionalGetReturnsNotModifiedWithoutBytes(t *testing.T) {
	var inm string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inm = r.Header.Get("If-None-Match")
		w.Header().Set("ETag", inm)
		w.WriteHeader(http.StatusNotModified)
	}))
	t.Cleanup(srv.Close)
	resp, err := noSleepClient(srv, nil).Do(context.Background(), "GET", "/v1/x", nil, map[string]string{"If-None-Match": `"v7"`})
	if err != nil || resp.StatusCode != 304 || len(resp.Body) != 0 || inm != `"v7"` {
		t.Fatalf("resp=%+v err=%v inm=%q", resp, err, inm)
	}
}

// CE08: la subida multipart llega con su boundary y sus bytes intactos, y un
// reintento tras 503 repite exactamente el mismo cuerpo y la misma clave.
func TestMultipartUploadRetriesWithSameBodyAndKey(t *testing.T) {
	logo := []byte("\x89PNG\r\n\x1a\n\x00logo\xff")
	path := filepath.Join(t.TempDir(), "logo.png")
	if err := os.WriteFile(path, logo, 0o600); err != nil {
		t.Fatal(err)
	}
	body, ct, err := MultipartBody(map[string]string{"kind": "logo"}, map[string]string{"file": path})
	if err != nil {
		t.Fatal(err)
	}
	srv, calls := scriptedServer(t,
		reply(503, "", nil, "Retry-After", "1"),
		reply(201, "application/json", []byte(`{"data":{"id":"a1"}}`)),
	)
	var slept []time.Duration
	resp, err := noSleepClient(srv, &slept).Do(context.Background(), "POST", "/v1/companies/acme_co/pdf-templates/assets", body, map[string]string{"Content-Type": ct})
	if err != nil {
		t.Fatal(err)
	}
	got := calls()
	if resp.StatusCode != 201 || len(got) != 2 {
		t.Fatalf("status=%d llamadas=%d", resp.StatusCode, len(got))
	}
	if got[0].ct != ct || got[1].ct != ct || !bytes.Equal(got[0].body, body) || !bytes.Equal(got[1].body, body) {
		t.Error("el reintento cambió el cuerpo o el boundary")
	}
	if got[0].idem == "" || got[0].idem != got[1].idem {
		t.Errorf("clave de idempotencia entre intentos: %q y %q", got[0].idem, got[1].idem)
	}
	if !bytes.Contains(got[1].body, logo) {
		t.Error("el multipart no contiene los bytes del fichero")
	}
	if len(slept) != 1 || slept[0] != time.Second {
		t.Errorf("espera = %v, quiero [1s] (Retry-After)", slept)
	}
}

// CE25: una escritura condicional reintentada mantiene If-Match, cuerpo y clave.
func TestRetriedConditionalWriteKeepsPreconditionBodyAndKey(t *testing.T) {
	srv, calls := scriptedServer(t,
		reply(429, "application/json", []byte(`{"error":{"type":"rate_limit_error"}}`), "Retry-After", "2"),
		reply(200, "application/json", []byte(`{"data":{}}`)),
	)
	body := []byte(`{"template_id":null,"expected_version":1}`)
	if _, err := noSleepClient(srv, nil).Do(context.Background(), "PATCH", "/v1/companies/acme_co/invoices/i/presentation", body, map[string]string{"If-Match": `"p1"`}); err != nil {
		t.Fatal(err)
	}
	got := calls()
	if len(got) != 2 {
		t.Fatalf("llamadas = %d", len(got))
	}
	for i, c := range got {
		if c.method != "PATCH" || c.ifMatch != `"p1"` || !bytes.Equal(c.body, body) {
			t.Errorf("intento %d: %+v", i, c)
		}
	}
	if got[0].idem == "" || got[0].idem != got[1].idem {
		t.Errorf("la clave cambió entre intentos: %q y %q", got[0].idem, got[1].idem)
	}
}

// CE25: 429 y 503 respetan Retry-After entre intentos y, agotados, exponen el
// valor y el request id en el error.
func TestRateLimitAndUnavailableHonourAndExposeRetryAfter(t *testing.T) {
	for _, status := range []int{429, 503} {
		srv, calls := scriptedServer(t, reply(status, "application/json", []byte(`{"error":{"type":"x","message":"m"}}`), "Retry-After", "9", "X-Request-Id", "req_1"))
		var slept []time.Duration
		_, err := noSleepClient(srv, &slept).Do(context.Background(), "GET", "/v1/x", nil, nil)
		var api *apierr.APIError
		if !errors.As(err, &api) || api.StatusCode != status || api.RetryAfter != "9" || api.RequestID != "req_1" {
			t.Fatalf("%d: error = %#v", status, err)
		}
		if len(calls()) != 4 || len(slept) != 3 || slept[0] != 9*time.Second {
			t.Errorf("%d: llamadas=%d esperas=%v", status, len(calls()), slept)
		}
	}
}

// CE15, CE25: el envío a FACe (cliente sin reintento) hace una sola llamada ante
// 429, 5xx y corte de conexión; los estados que el cliente nunca reintenta
// (409, 412, 428, 410, 422) tampoco se repiten en ningún cliente.
func TestSingleAttemptStatusesAndConnectionDrop(t *testing.T) {
	for _, status := range []int{429, 500, 502, 503} {
		srv, calls := scriptedServer(t, reply(status, "application/json", []byte(`{"error":{"type":"x"}}`)))
		shared := noSleepClient(srv, nil)
		if _, err := shared.WithoutRetry().Do(context.Background(), "POST", "/v1/companies/acme_co/invoices/i/face-submissions", []byte(`{}`), nil); err == nil {
			t.Fatalf("%d: esperaba error", status)
		}
		if n := len(calls()); n != 1 {
			t.Errorf("%d sin reintento: %d llamadas, quiero 1", status, n)
		}
	}

	for _, status := range []int{409, 410, 412, 422, 428} {
		srv, calls := scriptedServer(t, reply(status, "application/json", []byte(`{"error":{"type":"x","code":"c"}}`)))
		_, err := noSleepClient(srv, nil).Do(context.Background(), "PATCH", "/v1/x", []byte(`{}`), nil)
		var api *apierr.APIError
		if !errors.As(err, &api) || api.StatusCode != status || api.Code != "c" {
			t.Fatalf("%d: error = %#v", status, err)
		}
		if n := len(calls()); n != 1 {
			t.Errorf("%d: %d llamadas, quiero 1", status, n)
		}
	}

	var drops int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		drops++
		mu.Unlock()
		conn, _, _ := w.(http.Hijacker).Hijack()
		_ = conn.Close()
	}))
	t.Cleanup(srv.Close)
	c := noSleepClient(srv, nil)
	_, err := c.WithoutRetry().Do(context.Background(), "POST", "/v1/companies/acme_co/invoices/i/face-submissions", []byte(`{}`), nil)
	var transport *apierr.TransportError
	if !errors.As(err, &transport) {
		t.Fatalf("error = %#v, quiero TransportError", err)
	}
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return drops
	}
	if n := count(); n != 1 {
		t.Errorf("corte de conexión sin reintento: %d llamadas, quiero 1", n)
	}
	mu.Lock()
	drops = 0
	mu.Unlock()
	if _, err := c.Do(context.Background(), "GET", "/v1/x", nil, nil); err == nil || count() != 4 {
		t.Errorf("cliente normal: %d llamadas (err=%v), quiero 4", count(), err)
	}
}
