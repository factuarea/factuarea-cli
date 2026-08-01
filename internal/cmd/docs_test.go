package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

func runDocsSearch(t *testing.T, args ...string) string {
	t.Helper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"docs", "search"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("docs search %v devolvió error: %v", args, err)
	}
	return out.String()
}

func TestDocsSearchFindsInvoices(t *testing.T) {
	var hits []map[string]any
	if err := json.Unmarshal([]byte(runDocsSearch(t, "invoice", "--json")), &hits); err != nil {
		t.Fatalf("salida no es JSON: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("esperaba al menos una coincidencia para \"invoice\"")
	}
	found := false
	for _, h := range hits {
		if cmd, ok := h["command"].(string); ok && strings.Contains(cmd, "invoices") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("esperaba alguna entrada con \"invoices\" en command")
	}
}

func TestDocsSearchJSONShape(t *testing.T) {
	var hits []map[string]any
	if err := json.Unmarshal([]byte(runDocsSearch(t, "invoice", "--json")), &hits); err != nil {
		t.Fatalf("salida no es JSON: %v", err)
	}
	for i, h := range hits {
		for _, key := range []string{"command", "method", "path"} {
			if v, ok := h[key].(string); !ok || v == "" {
				t.Fatalf("entrada %d sin %q válido: %#v", i, key, h)
			}
		}
	}
}

func TestDocsSearchNoResults(t *testing.T) {
	var hits []map[string]any
	if err := json.Unmarshal([]byte(runDocsSearch(t, "zzzznotarealthing", "--json")), &hits); err != nil {
		t.Fatalf("salida no es JSON: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("esperaba 0 coincidencias, got %d", len(hits))
	}
}

// --- Documentación publicada: `docs list|grep|get` -------------------------
//
// El corpus se sirve desde un servidor INSTRUMENTADO que registra cada petición
// que recibe. Es lo que permite afirmar —y no suponer— que el término de
// búsqueda no sale de la máquina y que la descarga es anónima.

const corpusPath = "/llms-full.en.txt"

type recordedRequest struct {
	method     string
	path       string
	rawQuery   string
	requestURI string
	body       string
	header     http.Header
}

type docsServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
}

// newDocsServer sirve el fixture del corpus en CUALQUIER ruta: así, si un
// subcomando pidiera algo distinto del corpus, la petición quedaría registrada
// en vez de rebotar en un 404 que enmascararía la fuga.
func newDocsServer(t *testing.T) *docsServer {
	t.Helper()
	corpus, err := os.ReadFile(filepath.Join("..", "docs", "testdata", "corpus.md"))
	if err != nil {
		t.Fatalf("leer el fixture del corpus: %v", err)
	}
	ds := &docsServer{}
	ds.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ds.mu.Lock()
		ds.requests = append(ds.requests, recordedRequest{
			method:     r.Method,
			path:       r.URL.Path,
			rawQuery:   r.URL.RawQuery,
			requestURI: r.RequestURI,
			body:       string(body),
			header:     r.Header.Clone(),
		})
		ds.mu.Unlock()
		_, _ = w.Write(corpus)
	}))
	t.Cleanup(ds.Server.Close)
	return ds
}

func (ds *docsServer) recorded() []recordedRequest {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return append([]recordedRequest(nil), ds.requests...)
}

func (ds *docsServer) hits() int { return len(ds.recorded()) }

func (ds *docsServer) reset() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.requests = nil
}

func (ds *docsServer) corpusURL() string { return ds.URL + corpusPath }

// isolateDocsEnv deja el proceso sin credenciales ni configuración: HOME apunta
// a un temporal vacío, la API key y el perfil van vacíos, y la URL base de la
// API apunta al MISMO servidor instrumentado, de forma que cualquier llamada a
// la API quedaría registrada en vez de irse a producción sin dejar rastro.
func isolateDocsEnv(t *testing.T, srv *docsServer) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("FACTUAREA_API_KEY", "")
	t.Setenv("FACTUAREA_PROFILE", "")
	t.Setenv("FACTUAREA_DOCS_URL", srv.corpusURL())
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	return home
}

// runDocs ejecuta un subcomando con stdout y stderr SEPARADOS: el contrato dice
// que stdout es JSON parseable y que los avisos van por stderr.
func runDocs(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), errOut.String(), err
}

func docsEntries(t *testing.T, stdout string) []map[string]any {
	t.Helper()
	var entries []map[string]any
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		t.Fatalf("stdout no es JSON válido: %v\n%s", err, stdout)
	}
	return entries
}

// 3.4 — no-regresión: `docs search` resuelve del spec embebido y NO consulta el
// corpus de documentación, ni siquiera cuando el corpus está a un HTTP de
// distancia.
func TestDocsSearchNeverReadsTheDocsCorpus(t *testing.T) {
	srv := newDocsServer(t)
	home := isolateDocsEnv(t, srv)

	stdout, _, err := runDocs(t, "docs", "search", "invoice", "--json")
	if err != nil {
		t.Fatalf("docs search: %v", err)
	}
	if n := srv.hits(); n != 0 {
		t.Fatalf("docs search hizo %d peticiones; su fuente es el spec embebido en el binario", n)
	}
	if entries, readErr := os.ReadDir(filepath.Join(home, "cache", "factuarea", "docs")); readErr == nil && len(entries) != 0 {
		t.Fatalf("docs search no debe dejar caché de documentación, hay %d ficheros", len(entries))
	}

	hits := docsEntries(t, stdout)
	if len(hits) == 0 {
		t.Fatal("esperaba operaciones del spec embebido para \"invoice\"")
	}
	for i, h := range hits {
		for _, key := range []string{"command", "method", "path"} {
			if v, ok := h[key].(string); !ok || v == "" {
				t.Fatalf("entrada %d sin %q: %#v", i, key, h)
			}
		}
	}
}

// 3.5 — `docs list`
func TestDocsListEnumeratesCorpus(t *testing.T) {
	srv := newDocsServer(t)
	isolateDocsEnv(t, srv)

	stdout, _, err := runDocs(t, "docs", "list", "--json")
	if err != nil {
		t.Fatalf("docs list: %v", err)
	}
	entries := docsEntries(t, stdout)
	if len(entries) != 5 {
		t.Fatalf("el fixture tiene 5 páginas, listó %d", len(entries))
	}
	for i, e := range entries {
		for _, key := range []string{"path", "title"} {
			if v, ok := e[key].(string); !ok || v == "" {
				t.Fatalf("entrada %d sin %q: %#v", i, key, e)
			}
		}
	}

	human, _, err := runDocs(t, "docs", "list")
	if err != nil {
		t.Fatalf("docs list (humano): %v", err)
	}
	if !strings.Contains(human, "/guides/idempotency — Idempotency") {
		t.Fatalf("la salida humana debe ser `<ruta> — <título>`:\n%s", human)
	}
}

func TestDocsListFilteredByPrefix(t *testing.T) {
	srv := newDocsServer(t)
	isolateDocsEnv(t, srv)

	full := docsEntries(t, mustDocsStdout(t, "docs", "list", "--json"))
	filtered := docsEntries(t, mustDocsStdout(t, "docs", "list", "/guides", "--json"))

	if len(filtered) >= len(full) {
		t.Fatalf("el listado filtrado (%d) debe ser menor que el completo (%d)", len(filtered), len(full))
	}
	if len(filtered) != 2 {
		t.Fatalf("bajo /guides hay 2 páginas en el fixture, listó %d", len(filtered))
	}
	for _, e := range filtered {
		if path, _ := e["path"].(string); !strings.HasPrefix(path, "/guides") {
			t.Fatalf("%q no cuelga del prefijo pedido", path)
		}
	}
}

func TestDocsListOrderIsStable(t *testing.T) {
	srv := newDocsServer(t)
	isolateDocsEnv(t, srv)

	first := mustDocsStdout(t, "docs", "list", "--json")
	second := mustDocsStdout(t, "docs", "list", "--json")
	if first != second {
		t.Fatalf("el orden no es determinista:\n%s\n---\n%s", first, second)
	}
}

// La referencia de la API no se traduce: el corpus la emite una sola vez y sin
// prefijo de idioma, así que pedir `es` debe conservarla.
func TestDocsListLangKeepsUntranslatedAPIReference(t *testing.T) {
	srv := newDocsServer(t)
	isolateDocsEnv(t, srv)

	entries := docsEntries(t, mustDocsStdout(t, "docs", "list", "--lang", "es", "--json"))
	paths := map[string]bool{}
	for _, e := range entries {
		path, _ := e["path"].(string)
		paths[path] = true
	}
	if !paths["/es/guides/idempotency"] {
		t.Error("falta la guía en español")
	}
	if !paths["/api-reference/invoices/public-api.v1.invoices.list"] {
		t.Error("la operación de la API no lleva prefijo de idioma y debe seguir listándose con --lang es")
	}
	if paths["/guides/idempotency"] || paths["/ca/guides/idempotency"] {
		t.Errorf("--lang es no debe listar las guías de otros idiomas: %v", paths)
	}
}

func TestDocsListRejectsUnknownLang(t *testing.T) {
	srv := newDocsServer(t)
	isolateDocsEnv(t, srv)

	_, _, err := runDocs(t, "docs", "list", "--lang", "de")
	if err == nil {
		t.Fatal("un idioma no soportado debe ser error de uso")
	}
	if got := exit.ForError(err); got != exit.Usage {
		t.Fatalf("exit = %d, want Usage (%d)", got, exit.Usage)
	}
	if !strings.Contains(err.Error(), "idioma") {
		t.Fatalf("mensaje sin explicación en español: %v", err)
	}
}

// 3.6 — `docs grep`
func TestDocsGrepFindsGuideSection(t *testing.T) {
	srv := newDocsServer(t)
	isolateDocsEnv(t, srv)

	matches := docsEntries(t, mustDocsStdout(t, "docs", "grep", "idempotency-key", "--json"))
	if len(matches) == 0 {
		t.Fatal("esperaba coincidencias para \"idempotency-key\"")
	}
	guideHit, sectionHit := false, false
	for i, m := range matches {
		for _, key := range []string{"path", "title", "section", "snippet"} {
			if _, ok := m[key]; !ok {
				t.Fatalf("coincidencia %d sin la clave %q: %#v", i, key, m)
			}
		}
		if path, _ := m["path"].(string); path == "/guides/idempotency" {
			guideHit = true
			if snippet, _ := m["snippet"].(string); snippet == "" {
				t.Errorf("coincidencia %d sin fragmento de contexto", i)
			}
		}
		if section, _ := m["section"].(string); section != "" {
			sectionHit = true
		}
	}
	if !guideHit {
		t.Fatalf("ninguna coincidencia apunta a la guía de idempotencia: %#v", matches)
	}
	if !sectionHit {
		t.Error("grep devuelve SECCIONES: alguna coincidencia debe identificar su encabezado")
	}
}

func TestDocsGrepFindsAPIReferenceOperation(t *testing.T) {
	srv := newDocsServer(t)
	isolateDocsEnv(t, srv)

	const operation = "/api-reference/invoices/public-api.v1.invoices.list"

	byRoute := docsEntries(t, mustDocsStdout(t, "docs", "grep", "GET /v1/invoices", "--json"))
	found := false
	for _, m := range byRoute {
		if path, _ := m["path"].(string); path == operation {
			found = true
		}
	}
	if !found {
		t.Fatalf("buscar una ruta invocable debe devolver su operación de la referencia: %#v", byRoute)
	}

	// Un parámetro que solo existe en la referencia de la API: si el parser
	// hubiera descartado esas páginas, esto no encontraría nada.
	byParam := docsEntries(t, mustDocsStdout(t, "docs", "grep", "starting_after", "--json"))
	if len(byParam) == 0 {
		t.Fatal("el cuerpo de la referencia de la API debe ser buscable")
	}
	for _, m := range byParam {
		if path, _ := m["path"].(string); path != operation {
			t.Fatalf("\"starting_after\" solo está en la operación, pero coincidió %q", path)
		}
	}
}

func TestDocsGrepIsCaseInsensitive(t *testing.T) {
	srv := newDocsServer(t)
	isolateDocsEnv(t, srv)

	lower := mustDocsStdout(t, "docs", "grep", "idempotency", "--json")
	upper := mustDocsStdout(t, "docs", "grep", "IDEMPOTENCY", "--json")
	if lower != upper {
		t.Fatalf("la búsqueda debe ser insensible a mayúsculas:\n%s\n---\n%s", lower, upper)
	}
	if len(docsEntries(t, lower)) == 0 {
		t.Fatal("esperaba coincidencias para \"idempotency\"")
	}
}

func TestDocsGrepNoMatchesIsSuccess(t *testing.T) {
	srv := newDocsServer(t)
	isolateDocsEnv(t, srv)

	stdout, stderr, err := runDocs(t, "docs", "grep", "zzzznotarealthing", "--json")
	if err != nil {
		t.Fatalf("no encontrar nada NO es un error: %v", err)
	}
	if got := exit.ForError(err); got != exit.OK {
		t.Fatalf("exit = %d, want OK (0)", got)
	}
	if matches := docsEntries(t, stdout); len(matches) != 0 {
		t.Fatalf("esperaba 0 coincidencias, got %d", len(matches))
	}
	if strings.Contains(stderr, "zzzznotarealthing") && strings.TrimSpace(stdout) == "" {
		t.Fatal("el aviso no puede sustituir al documento JSON de stdout")
	}
}

// 3.7 — `docs get`
func TestDocsGetPrintsFullMarkdown(t *testing.T) {
	srv := newDocsServer(t)
	isolateDocsEnv(t, srv)

	stdout, _, err := runDocs(t, "docs", "get", "/guides/idempotency")
	if err != nil {
		t.Fatalf("docs get: %v", err)
	}
	for _, want := range []string{
		"# Idempotency (/guides/idempotency)",
		"## How it works",
		"## Key format",
		"Length between 1 and 255 characters.",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("la página completa debe incluir %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "Rate limits") {
		t.Error("get imprime UNA página, no el corpus entero")
	}
}

func TestDocsGetAcceptsPathWithoutLeadingSlash(t *testing.T) {
	srv := newDocsServer(t)
	isolateDocsEnv(t, srv)

	withSlash := mustDocsStdout(t, "docs", "get", "/guides/idempotency")
	withoutSlash := mustDocsStdout(t, "docs", "get", "guides/idempotency")
	if withSlash != withoutSlash {
		t.Fatal("la barra inicial es opcional: ambas invocaciones deben imprimir lo mismo")
	}
}

func TestDocsGetUnknownPageIsUsageError(t *testing.T) {
	srv := newDocsServer(t)
	isolateDocsEnv(t, srv)

	_, _, err := runDocs(t, "docs", "get", "/guides/no-existe")
	if err == nil {
		t.Fatal("una página inexistente debe fallar")
	}
	if got := exit.ForError(err); got != exit.Usage {
		t.Fatalf("exit = %d, want Usage (%d)", got, exit.Usage)
	}
	if !strings.Contains(err.Error(), "factuarea docs list") {
		t.Fatalf("el mensaje debe remitir al listado de páginas: %v", err)
	}
	if !strings.Contains(err.Error(), "no tiene ninguna página") {
		t.Fatalf("el mensaje debe estar en español: %v", err)
	}
}

// 3.8 — PRIVACIDAD: el diferencial del change. Los cuatro subcomandos funcionan
// sin credenciales y el término de búsqueda NO sale de la máquina.
func TestDocsSubcommandsAreCredentialFreeAndLeakNoQuery(t *testing.T) {
	const secret = "ClienteConfidencialZorrilla7731"

	srv := newDocsServer(t)
	isolateDocsEnv(t, srv)

	invocations := [][]string{
		{"docs", "search", secret, "--json"},
		{"docs", "list", "--json"},
		{"docs", "grep", secret, "--json"},
		{"docs", "get", "/guides/idempotency", "--json"},
	}
	for _, args := range invocations {
		stdout, stderr, err := runDocs(t, args...)
		if err != nil {
			t.Fatalf("%v falló sin credenciales: %v", args, err)
		}
		if !json.Valid([]byte(strings.TrimSpace(stdout))) {
			t.Fatalf("%v: stdout no es JSON válido:\n%s", args, stdout)
		}
		for _, forbidden := range []string{"API key", "api key", "credenciales", "factuarea login"} {
			if strings.Contains(stderr, forbidden) {
				t.Fatalf("%v: la documentación no necesita credenciales, pero stderr dice %q:\n%s", args, forbidden, stderr)
			}
		}
	}

	requests := srv.recorded()
	if len(requests) == 0 {
		t.Fatal("el corpus tenía que descargarse al menos una vez: sin peticiones el test no prueba nada")
	}
	needle := strings.ToLower(secret)
	for i, r := range requests {
		if r.path != corpusPath {
			t.Errorf("petición %d a %q; la ÚNICA ruta admisible es la del corpus (%s)", i, r.path, corpusPath)
		}
		for _, key := range []string{"Authorization", "X-Api-Key"} {
			if got := r.header.Get(key); got != "" {
				t.Errorf("petición %d llevó %s = %q; el corpus es público y se pide sin credenciales", i, key, got)
			}
		}
		for label, value := range map[string]string{
			"la URL":         r.requestURI,
			"la query":       r.rawQuery,
			"el cuerpo":      r.body,
			"las cabeceras":  fmt.Sprint(r.header),
			"el método+ruta": r.method + " " + r.path,
		} {
			if strings.Contains(strings.ToLower(value), needle) {
				t.Errorf("petición %d filtró el término de búsqueda en %s: %q", i, label, value)
			}
		}
	}
}

// 3.9 — con el corpus cacheado, la red sobra: sin ella los subcomandos responden
// igual y SIN aviso de copia vencida (que es lo que delataría un fetch fallido).
func TestDocsCachedCorpusNeedsNoNetwork(t *testing.T) {
	srv := newDocsServer(t)
	isolateDocsEnv(t, srv)

	if _, _, err := runDocs(t, "docs", "list", "--json"); err != nil {
		t.Fatalf("calentar la caché: %v", err)
	}
	if n := srv.hits(); n != 1 {
		t.Fatalf("la primera invocación debe descargar el corpus una vez, hizo %d peticiones", n)
	}

	srv.reset()
	srv.Close()

	for _, args := range [][]string{
		{"docs", "grep", "idempotency", "--json"},
		{"docs", "get", "/guides/idempotency"},
	} {
		stdout, stderr, err := runDocs(t, args...)
		if err != nil {
			t.Fatalf("%v con el corpus en caché y el servidor apagado: %v", args, err)
		}
		if strings.TrimSpace(stdout) == "" {
			t.Fatalf("%v no devolvió nada desde la caché", args)
		}
		if strings.Contains(stderr, "aviso") {
			t.Fatalf("%v avisó de copia desactualizada: la caché vigente no debe intentar la descarga:\n%s", args, stderr)
		}
	}
	if n := srv.hits(); n != 0 {
		t.Fatalf("con el corpus en caché no debe haber ninguna petición, hubo %d", n)
	}
}

func mustDocsStdout(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, err := runDocs(t, args...)
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, stderr)
	}
	return stdout
}
