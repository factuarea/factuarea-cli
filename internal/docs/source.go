// Package docs resuelve la documentación publicada de Factuarea en local: baja
// una vez el corpus `llms-full` del portal, lo cachea unos minutos y filtra
// sobre esa copia. Ningún argumento del usuario viaja a la red y ninguna
// petición lleva credenciales.
package docs

import (
	"os"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

const (
	// URLEnglish es el corpus por defecto: solo las páginas en inglés, que es
	// el idioma fuente del portal (D2).
	URLEnglish = "https://docs.factuarea.com/llms-full.en.txt"
	// URLCombined trae los tres idiomas. Se usa cuando se pide `es` o `ca`,
	// porque el portal no publica una variante por idioma distinta del inglés.
	URLCombined = "https://docs.factuarea.com/llms-full.txt"

	// EnvURL sobreescribe el origen del corpus, misma familia que
	// FACTUAREA_BASE_URL.
	EnvURL = "FACTUAREA_DOCS_URL"
)

// Source es el origen resuelto del corpus: de dónde bajarlo y qué páginas
// conservar de él.
type Source struct {
	// URL del corpus. Es fija (o la de EnvURL): NUNCA depende de los
	// argumentos del usuario.
	URL string
	// LocalePrefix restringe las páginas al idioma pedido: "/es/", "/ca/" o
	// "" cuando no hay nada que filtrar.
	LocalePrefix string
}

// Resolve traduce el idioma pedido a un origen. `en` (y el vacío) bajan el
// corpus inglés sin filtro; `es` y `ca` bajan el combinado y se quedan con las
// páginas de ese prefijo.
func Resolve(lang string) (Source, error) {
	var src Source
	switch lang {
	case "", "en":
		src = Source{URL: URLEnglish}
	case "es":
		src = Source{URL: URLCombined, LocalePrefix: "/es/"}
	case "ca":
		src = Source{URL: URLCombined, LocalePrefix: "/ca/"}
	default:
		return Source{}, apierr.Usagef("idioma %q no soportado en la documentación; usa en, es o ca", lang)
	}
	if override := os.Getenv(EnvURL); override != "" {
		src.URL = override
	}
	return src, nil
}
