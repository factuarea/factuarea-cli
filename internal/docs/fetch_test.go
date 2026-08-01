package docs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

// corpusServer sirve el corpus contando las peticiones y guardando las
// cabeceras de la última, que es lo que permite afirmar que la descarga es
// anónima.
type corpusServer struct {
	*httptest.Server
	hits    atomic.Int64
	headers atomic.Value // http.Header
	body    func(n int64) string
	status  int
}

func newCorpusServer(t *testing.T, status int, body func(n int64) string) *corpusServer {
	t.Helper()
	cs := &corpusServer{body: body, status: status}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := cs.hits.Add(1)
		cs.headers.Store(r.Header.Clone())
		w.WriteHeader(cs.status)
		_, _ = w.Write([]byte(cs.body(n)))
	}))
	t.Cleanup(cs.Server.Close)
	return cs
}

func (cs *corpusServer) lastHeaders() http.Header {
	h, _ := cs.headers.Load().(http.Header)
	return h
}

func newTestLoader(t *testing.T) *Loader {
	t.Helper()
	isolateCacheHome(t)
	l, err := NewLoader()
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	return l
}

func TestFetchSendsNoAuthorizationHeader(t *testing.T) {
	// La credencial está en el entorno a propósito: el corpus es un recurso
	// público y la descarga NO debe recogerla.
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	srv := newCorpusServer(t, http.StatusOK, func(int64) string { return "# corpus" })
	l := newTestLoader(t)

	if _, err := l.Load(context.Background(), Source{URL: srv.URL}, false); err != nil {
		t.Fatalf("Load: %v", err)
	}

	h := srv.lastHeaders()
	for _, key := range []string{"Authorization", "X-Api-Key"} {
		if got := h.Get(key); got != "" {
			t.Errorf("la descarga del corpus llevó %s = %q; debe ser anónima", key, got)
		}
	}
}

func TestFetchPersistsSuccessfulDownload(t *testing.T) {
	srv := newCorpusServer(t, http.StatusOK, func(int64) string { return "# corpus" })
	l := newTestLoader(t)

	got, err := l.Load(context.Background(), Source{URL: srv.URL}, false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Text != "# corpus" {
		t.Fatalf("texto = %q", got.Text)
	}
	if got.Stale {
		t.Fatal("una descarga con éxito no es una copia vencida")
	}

	cached, ok := l.Cache.Read(srv.URL)
	if !ok || cached.Text != "# corpus" {
		t.Fatalf("la descarga debe quedar persistida (ok=%v, texto=%q)", ok, cached.Text)
	}
}

func TestFetchSkipsNetworkWhileCacheIsFresh(t *testing.T) {
	srv := newCorpusServer(t, http.StatusOK, func(int64) string { return "# corpus" })
	l := newTestLoader(t)

	for i := 0; i < 3; i++ {
		if _, err := l.Load(context.Background(), Source{URL: srv.URL}, false); err != nil {
			t.Fatalf("Load %d: %v", i, err)
		}
	}
	if got := srv.hits.Load(); got != 1 {
		t.Fatalf("%d peticiones para tres invocaciones dentro del TTL; la caché no está evitando la red", got)
	}
}

func TestFetchRefreshRedownloadsOverFreshCopy(t *testing.T) {
	srv := newCorpusServer(t, http.StatusOK, func(n int64) string {
		if n == 1 {
			return "# corpus v1"
		}
		return "# corpus v2"
	})
	l := newTestLoader(t)

	if _, err := l.Load(context.Background(), Source{URL: srv.URL}, false); err != nil {
		t.Fatalf("Load inicial: %v", err)
	}
	got, err := l.Load(context.Background(), Source{URL: srv.URL}, true)
	if err != nil {
		t.Fatalf("Load con refresco: %v", err)
	}
	if got.Text != "# corpus v2" {
		t.Fatalf("el refresco forzado debe ignorar la copia vigente, texto = %q", got.Text)
	}
	if n := srv.hits.Load(); n != 2 {
		t.Fatalf("%d peticiones, esperaba 2 (la inicial y la forzada)", n)
	}
}

func TestFetchServesExpiredCopyWhenServerIsDown(t *testing.T) {
	srv := newCorpusServer(t, http.StatusOK, func(int64) string { return "# corpus viejo" })
	l := newTestLoader(t)

	if _, err := l.Load(context.Background(), Source{URL: srv.URL}, false); err != nil {
		t.Fatalf("Load inicial: %v", err)
	}
	stale := time.Now().Add(-TTL - time.Minute)
	if err := os.Chtimes(l.Cache.PathFor(srv.URL), stale, stale); err != nil {
		t.Fatalf("envejecer la copia: %v", err)
	}
	srv.Close()

	got, err := l.Load(context.Background(), Source{URL: srv.URL}, false)
	if err != nil {
		t.Fatalf("con copia en disco, un fallo de red NO debe ser error: %v", err)
	}
	if !got.Stale {
		t.Fatal("la copia servida tras el fallo debe marcarse como vencida para poder avisar")
	}
	if got.Text != "# corpus viejo" {
		t.Fatalf("texto = %q", got.Text)
	}
}

func TestFetchFailsAsTransportErrorWithoutCache(t *testing.T) {
	srv := newCorpusServer(t, http.StatusOK, func(int64) string { return "# corpus" })
	srv.Close()
	l := newTestLoader(t)

	_, err := l.Load(context.Background(), Source{URL: srv.URL}, false)
	if err == nil {
		t.Fatal("sin copia en disco y sin red, Load debe fallar")
	}
	var transport *apierr.TransportError
	if !errors.As(err, &transport) {
		t.Fatalf("error = %T (%v); debe ser TransportError para mapear a exit 10", err, err)
	}
}

func TestFetchNonSuccessStatusWritesNothing(t *testing.T) {
	srv := newCorpusServer(t, http.StatusServiceUnavailable, func(int64) string { return "upstream caído" })
	l := newTestLoader(t)

	_, err := l.Load(context.Background(), Source{URL: srv.URL}, false)
	if err == nil {
		t.Fatal("un estado no-2xx sin copia previa debe fallar")
	}
	var transport *apierr.TransportError
	if !errors.As(err, &transport) {
		t.Fatalf("error = %T (%v); debe ser TransportError para mapear a exit 10", err, err)
	}
	if entries, dirErr := os.ReadDir(l.Cache.dir); dirErr == nil && len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("un cuerpo de error NO puede quedar cacheado como corpus, hay: %v", names)
	}
}
