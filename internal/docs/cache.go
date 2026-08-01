package docs

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/factuarea/factuarea-cli/internal/config"
)

// TTL es la ventana durante la que la copia en disco se considera vigente. El
// portal sirve el corpus con `max-age=300`, así que 15 minutos no entrega nada
// radicalmente más viejo que un proxy intermedio, y una sesión que encadene
// varios subcomandos descarga una sola vez.
const TTL = 15 * time.Minute

// Cache guarda una copia del corpus por origen en el directorio de caché del
// usuario.
type Cache struct{ dir string }

// NewCache resuelve <CacheDir>/docs. No crea el directorio: eso lo hace Write,
// para que leer sobre una máquina sin caché no deje rastro.
func NewCache() (*Cache, error) {
	base, err := config.CacheDir()
	if err != nil {
		return nil, err
	}
	return &Cache{dir: filepath.Join(base, "docs")}, nil
}

// PathFor devuelve el fichero donde vive la copia de ese origen. La clave es un
// hash corto de la URL, así que dos variantes del corpus no se pisan.
func (c *Cache) PathFor(url string) string {
	sum := sha256.Sum256([]byte(url))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:8])+".md")
}

// CachedCorpus es una copia en disco: su contenido y si sigue vigente.
type CachedCorpus struct {
	Text  string
	Fresh bool
}

// Read devuelve la copia cacheada de ese origen. El segundo valor es false
// cuando no hay copia; una copia expirada SÍ se devuelve (con Fresh=false),
// porque es la que salva el escenario de red caída.
func (c *Cache) Read(url string) (CachedCorpus, bool) {
	path := c.PathFor(url)
	info, err := os.Stat(path)
	if err != nil {
		return CachedCorpus{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return CachedCorpus{}, false
	}
	return CachedCorpus{Text: string(b), Fresh: time.Since(info.ModTime()) < TTL}, true
}

// Write reemplaza la copia de forma atómica: escribe un temporal en el mismo
// directorio y lo renombra, para que una descarga interrumpida nunca deje un
// corpus truncado en su sitio.
func (c *Cache) Write(url string, body []byte) error {
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(c.dir, ".corpus-*.md.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, c.PathFor(url)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
