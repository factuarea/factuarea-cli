package docs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

// fetchTimeout acota la descarga del corpus, que son varios MB de texto.
const fetchTimeout = 30 * time.Second

// Corpus es el texto del corpus resuelto y si procede de una copia vencida.
type Corpus struct {
	Text string
	// Stale es true cuando la descarga falló y se está sirviendo una copia
	// expirada del disco. Quien lo consuma debe avisarlo por stderr.
	Stale bool
}

// Loader baja el corpus y lo cachea. El http.Client es propio y deliberado: el
// de internal/client siempre pone `Authorization: Bearer`, y el corpus es un
// recurso público que NO debe pedirse con credenciales.
type Loader struct {
	Cache  *Cache
	Client *http.Client
}

// NewLoader construye el Loader por defecto.
func NewLoader() (*Loader, error) {
	cache, err := NewCache()
	if err != nil {
		return nil, err
	}
	return &Loader{Cache: cache, Client: &http.Client{Timeout: fetchTimeout}}, nil
}

// Load resuelve el corpus de ese origen. Con una copia vigente no toca la red;
// si toca descargar y la descarga falla, cae a la copia en disco aunque esté
// expirada (Stale=true) y solo falla cuando no hay ninguna.
func (l *Loader) Load(ctx context.Context, src Source, refresh bool) (Corpus, error) {
	cached, hasCached := l.Cache.Read(src.URL)
	if hasCached && cached.Fresh && !refresh {
		return Corpus{Text: cached.Text}, nil
	}

	body, err := l.download(ctx, src.URL)
	if err != nil {
		if hasCached {
			return Corpus{Text: cached.Text, Stale: true}, nil
		}
		return Corpus{}, err
	}
	// Un fallo al persistir no invalida el corpus recién descargado: el
	// comando responde igual y la próxima invocación reintentará.
	_ = l.Cache.Write(src.URL, body)
	return Corpus{Text: string(body)}, nil
}

func (l *Loader) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, apierr.Usagef("la URL del corpus de documentación (%s) no es válida: %v", url, err)
	}
	resp, err := l.Client.Do(req)
	if err != nil {
		return nil, &apierr.TransportError{Err: fmt.Errorf("no se pudo descargar la documentación desde %s: %w. Revisa tu conexión y reinténtalo", url, err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &apierr.TransportError{Err: fmt.Errorf("la documentación en %s respondió %s. Reinténtalo en unos minutos", url, resp.Status)}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &apierr.TransportError{Err: fmt.Errorf("se cortó la descarga de la documentación desde %s: %w", url, err)}
	}
	return body, nil
}
