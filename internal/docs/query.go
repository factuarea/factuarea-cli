package docs

import (
	"sort"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

// snippetWidth acota el fragmento de contexto de cada coincidencia, en runas.
const snippetWidth = 160

// Entry es una página en el listado.
type Entry struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

// Match es una sección que coincide con la búsqueda.
type Match struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Section string `json:"section"`
	Snippet string `json:"snippet"`
}

// inLocale decide si una página pertenece al idioma pedido.
//
// Las guías llevan el prefijo del locale en su ruta, pero la referencia de la
// API NO se traduce: el corpus la emite una sola vez y sin prefijo. Filtrar por
// prefijo a secas dejaría fuera las operaciones justo cuando se pide `es` o
// `ca`, así que se conservan siempre.
func inLocale(page Page, localePrefix string) bool {
	if localePrefix == "" || page.Kind == KindOperation {
		return true
	}
	// La portada de cada idioma es `/es`, sin barra final: comparar solo por
	// `/es/` la dejaría fuera.
	return page.Path == strings.TrimSuffix(localePrefix, "/") || strings.HasPrefix(page.Path, localePrefix)
}

// List enumera las páginas del corpus, opcionalmente restringidas a las que
// cuelgan de un prefijo de ruta. El orden es alfabético por ruta.
func List(pages []Page, localePrefix, pathPrefix string) []Entry {
	prefix := normalizePath(pathPrefix)
	entries := []Entry{}
	for _, page := range pages {
		if page.Path == "" || !inLocale(page, localePrefix) {
			continue
		}
		if pathPrefix != "" && !strings.HasPrefix(page.Path, prefix) {
			continue
		}
		entries = append(entries, Entry{Path: page.Path, Title: page.Title})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Path != entries[j].Path {
			return entries[i].Path < entries[j].Path
		}
		return entries[i].Title < entries[j].Title
	})
	return entries
}

// Grep devuelve las secciones que contienen el término, insensible a
// mayúsculas. Sin coincidencias devuelve una lista vacía, que no es un error.
func Grep(pages []Page, localePrefix, query string) []Match {
	needle := strings.ToLower(query)
	matches := []Match{}
	for _, page := range pages {
		if !inLocale(page, localePrefix) {
			continue
		}
		titleHit := strings.Contains(strings.ToLower(page.Title), needle)
		hitInSections := false
		for _, section := range page.Sections {
			headingHit := strings.Contains(strings.ToLower(section.Heading), needle)
			bodyHit := strings.Contains(strings.ToLower(section.Body), needle)
			if !headingHit && !bodyHit {
				continue
			}
			hitInSections = true
			matches = append(matches, Match{
				Path:    page.Path,
				Title:   page.Title,
				Section: section.Heading,
				Snippet: snippetAround(section.Body, needle, bodyHit),
			})
		}
		// Una coincidencia solo en el título sigue siendo una coincidencia de
		// la página, aunque ninguna sección la contenga.
		if titleHit && !hitInSections {
			body := ""
			section := ""
			if len(page.Sections) > 0 {
				body = page.Sections[0].Body
				section = page.Sections[0].Heading
			}
			matches = append(matches, Match{
				Path:    page.Path,
				Title:   page.Title,
				Section: section,
				Snippet: snippetAround(body, needle, false),
			})
		}
	}
	return matches
}

// Get devuelve la página completa por su ruta canónica, con o sin barra
// inicial.
func Get(pages []Page, localePrefix, path string) (Page, error) {
	wanted := normalizePath(path)
	for _, page := range pages {
		if page.Path == wanted && inLocale(page, localePrefix) {
			return page, nil
		}
	}
	return Page{}, apierr.Usagef("la documentación no tiene ninguna página en %s. Lista las disponibles con `factuarea docs list`", wanted)
}

// normalizePath acepta la ruta con y sin barra inicial, y tolera la barra
// final salvo en la raíz, que ES una página del portal.
func normalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

// snippetAround recorta el contexto alrededor de la coincidencia. Si el término
// no está en el cuerpo (coincidió el encabezado o el título), sirve el arranque
// de la sección.
func snippetAround(body, needle string, found bool) string {
	text := strings.Join(strings.Fields(body), " ")
	runes := []rune(text)
	if len(runes) <= snippetWidth {
		return text
	}
	start := 0
	if found {
		if at := strings.Index(strings.ToLower(text), needle); at > 0 {
			// Índice en bytes → posición en runas, para no cortar a mitad de
			// un carácter multibyte.
			at = len([]rune(text[:at]))
			start = at - snippetWidth/2
			if start < 0 {
				start = 0
			}
		}
	}
	end := start + snippetWidth
	if end > len(runes) {
		end = len(runes)
		start = end - snippetWidth
	}
	snippet := string(runes[start:end])
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(runes) {
		snippet += "…"
	}
	return snippet
}
