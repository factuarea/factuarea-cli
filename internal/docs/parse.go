package docs

import (
	"net/url"
	"regexp"
	"strings"
)

// Kind distingue los dos tipos de página que emite el corpus, que no comparten
// formato de cabecera.
type Kind int

const (
	// KindGuide es una página escrita a mano del portal. Existe en los tres
	// idiomas, con su prefijo de locale en la ruta.
	KindGuide Kind = iota
	// KindOperation es una operación de la referencia de la API, renderizada
	// desde el spec OpenAPI. El corpus la emite UNA sola vez porque es
	// idéntica en los tres idiomas, así que su ruta no lleva prefijo.
	KindOperation
)

// Section es un tramo de página delimitado por un encabezado de nivel ≥ 2. La
// sección con Heading vacío es la introducción implícita: lo que va entre el
// título de la página y su primer encabezado.
type Section struct {
	Heading string
	Body    string
}

// Page es una página del corpus.
type Page struct {
	Title string
	// Path es la ruta canónica en el portal, con barra inicial. Puede quedar
	// vacía si la cabecera es reconocible pero la ruta no (página buscable
	// pero no listable ni obtenible).
	Path     string
	Kind     Kind
	Body     string
	Sections []Section
}

// El corpus tiene DOS formatos de cabecera de página y hay que reconocer los
// dos: exigir la ruta entre paréntesis descartaría las ~403 operaciones de la
// API, que es el contenido de más valor. `^# ` a secas tampoco vale: hay
// cabeceras de ese nivel que son contenido interno de una página.
var (
	// `# Idempotency (/guides/idempotency)`
	guideHeaderRe = regexp.MustCompile(`^# (.*\S) \((/[^()]*)\)[ \t]*$`)
	// `# GET /v1/invoices — List all invoices` (el resumen es opcional).
	operationHeaderRe = regexp.MustCompile(`^# (GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS) (/\S*)`)
	// La ruta de una operación solo se recupera de su propio cuerpo.
	docsBulletRe = regexp.MustCompile(`^- \*\*Docs\*\*: (\S+)[ \t]*$`)
	// Encabezado de sección: nivel 2 o más.
	sectionHeaderRe = regexp.MustCompile(`^(#{2,6}) +(.*\S)[ \t]*$`)
	// Sufijo de ancla que añade el portal: `## Cómo funciona [#como-funciona]`.
	anchorSuffixRe = regexp.MustCompile(`\s*\[#[^\]]*\]$`)
)

// Parse trocea el corpus en páginas, en el orden en que vienen.
//
// Una página empieza en una cabecera reconocible que va precedida por una línea
// `---` a nivel raíz (o que abre el corpus). Todo lo anterior a la primera
// página —la cabecera del propio corpus— se descarta, y una línea `---` que no
// vaya seguida de cabecera válida es contenido de la página en curso.
func Parse(corpus string) []Page {
	lines := strings.Split(corpus, "\n")

	starts := []int{}
	for i, line := range lines {
		if !isPageHeader(line) {
			continue
		}
		if i == 0 || precededBySeparator(lines, i) {
			starts = append(starts, i)
		}
	}

	pages := make([]Page, 0, len(starts))
	for n, start := range starts {
		end := len(lines)
		if n+1 < len(starts) {
			// La cabecera siguiente viene precedida por su separador y por
			// las líneas en blanco que lo rodean; nada de eso es cuerpo.
			end = separatorBefore(lines, starts[n+1])
		}
		if page, ok := parsePage(lines[start:end]); ok {
			pages = append(pages, page)
		}
	}
	return pages
}

func isPageHeader(line string) bool {
	return guideHeaderRe.MatchString(line) || operationHeaderRe.MatchString(line)
}

// precededBySeparator comprueba que la cabecera va detrás de un `---` a nivel
// raíz, saltando las líneas en blanco que el corpus intercala.
func precededBySeparator(lines []string, header int) bool {
	return separatorBefore(lines, header) >= 0
}

// separatorBefore devuelve el índice de la línea `---` que precede a la
// cabecera, o -1 si no la hay.
func separatorBefore(lines []string, header int) int {
	for i := header - 1; i >= 0; i-- {
		switch {
		case strings.TrimSpace(lines[i]) == "":
			continue
		case lines[i] == "---":
			return i
		default:
			return -1
		}
	}
	return -1
}

func parsePage(block []string) (Page, bool) {
	if len(block) == 0 {
		return Page{}, false
	}
	page := Page{}
	header := block[0]
	switch {
	case guideHeaderRe.MatchString(header):
		m := guideHeaderRe.FindStringSubmatch(header)
		page.Kind = KindGuide
		page.Title = m[1]
		page.Path = m[2]
	case operationHeaderRe.MatchString(header):
		page.Kind = KindOperation
		page.Title = strings.TrimSpace(strings.TrimPrefix(header, "# "))
		page.Path = operationPath(block)
	default:
		return Page{}, false
	}

	body := block
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}
	page.Body = strings.Join(body, "\n")
	page.Sections = parseSections(body[1:])
	return page, true
}

// operationPath recupera la ruta canónica de una operación de su bullet
// `- **Docs**:`, el único sitio del corpus donde aparece.
func operationPath(block []string) string {
	for _, line := range block {
		m := docsBulletRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if u, err := url.Parse(m[1]); err == nil {
			return u.Path
		}
		return ""
	}
	return ""
}

func parseSections(body []string) []Section {
	sections := []Section{}
	heading := ""
	var buf []string
	flush := func() {
		text := strings.Trim(strings.Join(buf, "\n"), "\n")
		// La introducción implícita solo cuenta si tiene contenido; una
		// sección con encabezado se emite aunque venga vacía.
		if heading == "" && strings.TrimSpace(text) == "" {
			return
		}
		sections = append(sections, Section{Heading: heading, Body: text})
	}
	for _, line := range body {
		if m := sectionHeaderRe.FindStringSubmatch(line); m != nil {
			flush()
			heading = strings.TrimSpace(anchorSuffixRe.ReplaceAllString(m[2], ""))
			buf = nil
			continue
		}
		buf = append(buf, line)
	}
	flush()
	return sections
}
