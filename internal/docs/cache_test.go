package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testCorpusURL = "https://docs.factuarea.example/llms-full.en.txt"

// isolateCacheHome aparta la caché del usuario real: `os.UserCacheDir` mira HOME
// en macOS y XDG_CACHE_HOME (o HOME) en Linux, así que se fijan los dos.
func isolateCacheHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	return home
}

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	home := isolateCacheHome(t)

	c, err := NewCache()
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	if !strings.HasPrefix(c.dir, home) {
		t.Fatalf("la caché de test apunta a %q, fuera del HOME temporal %q: escribiría en la caché real del usuario", c.dir, home)
	}
	return c
}

func tempFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("listar %s: %v", dir, err)
	}
	leftovers := []string{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			leftovers = append(leftovers, e.Name())
		}
	}
	return leftovers
}

func TestCacheWriteIsAtomicAndLeavesNoTempFile(t *testing.T) {
	c := newTestCache(t)

	if err := c.Write(testCorpusURL, []byte("# corpus")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if left := tempFilesIn(t, c.dir); len(left) != 0 {
		t.Fatalf("una escritura con éxito no debe dejar temporales, quedan: %v", left)
	}
	if _, err := os.Stat(c.PathFor(testCorpusURL)); err != nil {
		t.Fatalf("el corpus debe quedar en su ruta definitiva: %v", err)
	}
}

func TestCacheReadWithinTTLIsFresh(t *testing.T) {
	c := newTestCache(t)
	if err := c.Write(testCorpusURL, []byte("# corpus")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, ok := c.Read(testCorpusURL)
	if !ok {
		t.Fatal("Read debe encontrar la copia recién escrita")
	}
	if got.Text != "# corpus" {
		t.Fatalf("contenido = %q", got.Text)
	}
	if !got.Fresh {
		t.Fatal("una copia recién escrita está dentro del TTL y debe ser vigente")
	}
}

func TestCacheReadExpiresByModTime(t *testing.T) {
	c := newTestCache(t)
	if err := c.Write(testCorpusURL, []byte("# corpus")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	stale := time.Now().Add(-TTL - time.Minute)
	if err := os.Chtimes(c.PathFor(testCorpusURL), stale, stale); err != nil {
		t.Fatalf("envejecer la copia: %v", err)
	}

	got, ok := c.Read(testCorpusURL)
	if !ok {
		t.Fatal("una copia expirada SIGUE devolviéndose: es la que salva la red caída")
	}
	if got.Fresh {
		t.Fatalf("una copia de hace %v debe estar expirada (TTL = %v)", TTL+time.Minute, TTL)
	}
	if got.Text != "# corpus" {
		t.Fatalf("contenido = %q", got.Text)
	}
}

func TestCacheReadMissingCopy(t *testing.T) {
	c := newTestCache(t)

	if _, ok := c.Read(testCorpusURL); ok {
		t.Fatal("sin copia en disco, Read debe indicar que no hay nada")
	}
	if _, err := os.Stat(c.dir); !os.IsNotExist(err) {
		t.Fatalf("leer sin caché no debe crear el directorio (err = %v)", err)
	}
}

// Una escritura cortada a la mitad deja su temporal en el directorio, nunca en
// la ruta definitiva: el corpus anterior sigue siendo el que se lee.
func TestCacheInterruptedWriteLeavesPreviousCopyIntact(t *testing.T) {
	c := newTestCache(t)
	if err := c.Write(testCorpusURL, []byte("# corpus completo")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	partial := filepath.Join(c.dir, ".corpus-interrumpido.md.tmp")
	if err := os.WriteFile(partial, []byte("# corpus trun"), 0o600); err != nil {
		t.Fatalf("simular la escritura interrumpida: %v", err)
	}

	got, ok := c.Read(testCorpusURL)
	if !ok {
		t.Fatal("Read debe seguir encontrando la copia buena")
	}
	if got.Text != "# corpus completo" {
		t.Fatalf("el corpus se ha corrompido con el temporal: %q", got.Text)
	}
}

func TestCacheWriteFailureRemovesItsTempFile(t *testing.T) {
	c := newTestCache(t)
	if err := os.MkdirAll(c.PathFor(testCorpusURL), 0o700); err != nil {
		t.Fatalf("ocupar la ruta destino: %v", err)
	}

	// Con un directorio ocupando la ruta destino el rename no puede completarse.
	if err := c.Write(testCorpusURL, []byte("# corpus")); err == nil {
		t.Fatal("Write debe fallar si no puede colocar el corpus en su ruta")
	}
	if left := tempFilesIn(t, c.dir); len(left) != 0 {
		t.Fatalf("una escritura fallida debe limpiar su temporal, quedan: %v", left)
	}
}

func TestCacheKeysDifferentSourcesApart(t *testing.T) {
	c := newTestCache(t)

	if c.PathFor(URLEnglish) == c.PathFor(URLCombined) {
		t.Fatal("las dos variantes del corpus no pueden compartir fichero de caché")
	}
	if c.PathFor(URLEnglish) != c.PathFor(URLEnglish) {
		t.Fatal("la clave de caché debe ser estable entre invocaciones")
	}
}
