package docs

import (
	"os"
	"strings"
	"testing"
)

// fixtureCorpus es el recorte congelado del corpus real: la cabecera del propio
// corpus, tres guías (una por idioma), una operación de la referencia de la API
// con su formato REAL de cabecera, y una página cuyo cuerpo contiene una línea
// `---` que no separa páginas.
func fixtureCorpus(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/corpus.md")
	if err != nil {
		t.Fatalf("leer el fixture del corpus: %v", err)
	}
	return string(b)
}

func TestParseCountsPagesOfBothHeaderFormats(t *testing.T) {
	pages := Parse(fixtureCorpus(t))

	want := []Page{
		{Title: "Idempotency", Path: "/guides/idempotency", Kind: KindGuide},
		{Title: "Idempotencia", Path: "/es/guides/idempotency", Kind: KindGuide},
		{Title: "Idempotència", Path: "/ca/guides/idempotency", Kind: KindGuide},
		{Title: "GET /v1/invoices — List all invoices", Path: "/api-reference/invoices/public-api.v1.invoices.list", Kind: KindOperation},
		{Title: "Rate limits", Path: "/guides/rate-limits", Kind: KindGuide},
	}
	if len(pages) != len(want) {
		got := make([]string, len(pages))
		for i, p := range pages {
			got[i] = p.Path
		}
		t.Fatalf("el corpus tiene %d páginas, esperaba %d: %v", len(pages), len(want), got)
	}
	for i, w := range want {
		if pages[i].Title != w.Title || pages[i].Path != w.Path || pages[i].Kind != w.Kind {
			t.Errorf("página %d = {%q, %q, kind %d}, esperaba {%q, %q, kind %d}",
				i, pages[i].Title, pages[i].Path, pages[i].Kind, w.Title, w.Path, w.Kind)
		}
	}
}

// La referencia de la API es el contenido de más valor del corpus y su cabecera
// NO lleva la ruta entre paréntesis: si el parser exigiera ese formato, las
// operaciones desaparecerían en silencio.
func TestParseRecoversOperationPathFromDocsBullet(t *testing.T) {
	pages := Parse(fixtureCorpus(t))

	var op *Page
	for i := range pages {
		if pages[i].Kind == KindOperation {
			op = &pages[i]
			break
		}
	}
	if op == nil {
		t.Fatal("el corpus del fixture tiene una operación de la API y el parser no la reconoció")
	}
	if op.Path != "/api-reference/invoices/public-api.v1.invoices.list" {
		t.Errorf("ruta de la operación = %q; sale del bullet `- **Docs**:`, que es el único sitio donde aparece", op.Path)
	}
	if !strings.Contains(op.Body, "`public-api.v1.invoices.list`") {
		t.Error("el cuerpo de la operación debe conservar sus bullets de metadatos")
	}
}

func TestParseDoesNotSplitPageOnInlineSeparator(t *testing.T) {
	pages := Parse(fixtureCorpus(t))

	last := pages[len(pages)-1]
	if last.Path != "/guides/rate-limits" {
		t.Fatalf("la última página es %q; el `---` de su cuerpo la ha partido", last.Path)
	}
	// El `---` va seguido de `# Deprecated header`, que no es una cabecera de
	// página válida: ambos son cuerpo de esta página.
	if !strings.Contains(last.Body, "\n---\n") {
		t.Error("el `---` del cuerpo debe conservarse dentro de la página")
	}
	if !strings.Contains(last.Body, "# Deprecated header") {
		t.Error("el `# ` que sigue al `---` es contenido interno y debe seguir en el cuerpo")
	}
	for _, p := range pages {
		if p.Title == "Deprecated header" {
			t.Fatal("`# Deprecated header` no es inicio de página: no va precedido de un `---` con cabecera válida")
		}
	}
}

func TestParseDropsCorpusPreamble(t *testing.T) {
	pages := Parse(fixtureCorpus(t))

	if pages[0].Path != "/guides/idempotency" {
		t.Fatalf("la primera página es %q; la cabecera del corpus se ha colado", pages[0].Path)
	}
	for _, p := range pages {
		if strings.Contains(p.Body, "Concatenated Markdown export") {
			t.Fatalf("la cabecera del corpus aparece dentro de la página %q", p.Path)
		}
	}
}

func TestParseSectionsIncludeImplicitIntro(t *testing.T) {
	pages := Parse(fixtureCorpus(t))

	guide := pages[0]
	if got := len(guide.Sections); got != 3 {
		t.Fatalf("%q tiene %d secciones, esperaba 3 (intro implícita + 2 encabezados)", guide.Path, got)
	}
	intro := guide.Sections[0]
	if intro.Heading != "" {
		t.Errorf("la primera sección debe ser la intro implícita, tiene encabezado %q", intro.Heading)
	}
	if !strings.Contains(intro.Body, "Write operations") {
		t.Errorf("la intro implícita debe recoger el texto previo al primer encabezado, got %.60q", intro.Body)
	}
	// El sufijo de ancla que añade el portal no forma parte del encabezado.
	if guide.Sections[1].Heading != "How it works" || guide.Sections[2].Heading != "Key format" {
		t.Errorf("encabezados = %q y %q", guide.Sections[1].Heading, guide.Sections[2].Heading)
	}
	if strings.Contains(guide.Sections[1].Body, "## Key format") {
		t.Error("una sección no debe arrastrar el encabezado de la siguiente")
	}
}
