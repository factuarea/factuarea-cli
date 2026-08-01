package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/docs"
	"github.com/spf13/cobra"
)

type docsMatch struct {
	Command string `json:"command"`
	Summary string `json:"summary"`
	Method  string `json:"method"`
	Path    string `json:"path"`
}

func newDocsCmd() *cobra.Command {
	group := &cobra.Command{
		Use:   "docs",
		Short: "Documentación en local y sin credenciales: operaciones del spec embebido y páginas del portal en caché",
		Args:  UsageArgs(cobra.NoArgs),
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	group.AddCommand(newDocsSearchCmd())
	group.AddCommand(newDocsListCmd(), newDocsGrepCmd(), newDocsGetCmd())
	return group
}

func newDocsSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Busca operaciones de la API en la referencia embebida",
		Args:  UsageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.ToLower(args[0])
			matches := []docsMatch{}
			for _, op := range generatedOps() {
				command := commandPath(op)
				haystack := strings.ToLower(command + " " + op.Summary + " " + op.Path)
				if !strings.Contains(haystack, query) {
					continue
				}
				matches = append(matches, docsMatch{
					Command: command,
					Summary: op.Summary,
					Method:  op.Method,
					Path:    op.Path,
				})
			}

			if globalsFrom(cmd).JSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetEscapeHTML(false)
				return enc.Encode(matches)
			}

			if len(matches) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Sin coincidencias para %q.\n", args[0])
				return nil
			}
			w := cmd.OutOrStdout()
			for _, m := range matches {
				fmt.Fprintf(w, "%s — %s  (%s %s)\n", m.Command, m.Summary, m.Method, m.Path)
			}
			return nil
		},
	}
}

// corpusFlags son las opciones de los subcomandos que consultan la
// documentación publicada. NO se declaran como persistentes del grupo a
// propósito: `docs search` resuelve desde el spec embebido y no las usa, así
// que aparecer en su ayuda solo confundiría.
type corpusFlags struct {
	lang    string
	refresh bool
}

func (f *corpusFlags) bind(c *cobra.Command) {
	c.Flags().StringVar(&f.lang, "lang", "en", "idioma de la documentación: en, es o ca")
	c.Flags().BoolVar(&f.refresh, "refresh", false, "fuerza la descarga del corpus ignorando la caché vigente")
}

// loadDocsCorpus resuelve el corpus de documentación y lo trocea en páginas.
//
// La única petición de red posible es la descarga del corpus completo, con una
// URL que no depende de los argumentos: ni el término de búsqueda ni la página
// pedida salen de la máquina. Tampoco se construye cliContext, así que no se
// resuelve ni se envía ninguna credencial.
func loadDocsCorpus(cmd *cobra.Command, f corpusFlags) ([]docs.Page, string, error) {
	src, err := docs.Resolve(f.lang)
	if err != nil {
		return nil, "", err
	}
	loader, err := docs.NewLoader()
	if err != nil {
		return nil, "", apierr.Usagef("no se pudo determinar el directorio de caché del sistema: %v", err)
	}
	corpus, err := loader.Load(cmd.Context(), src, f.refresh)
	if err != nil {
		return nil, "", err
	}
	if corpus.Stale {
		// Por stderr, para no romper el JSON de stdout que consume un agente.
		fmt.Fprintln(cmd.ErrOrStderr(), "aviso: no se pudo actualizar la documentación; se muestra la copia en caché, que puede estar desactualizada.")
	}
	return docs.Parse(corpus.Text), src.LocalePrefix, nil
}

func newDocsListCmd() *cobra.Command {
	var f corpusFlags
	c := &cobra.Command{
		Use:   "list [prefijo]",
		Short: "Lista las páginas de la documentación publicada (corpus en caché)",
		Args:  UsageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			pages, locale, err := loadDocsCorpus(cmd, f)
			if err != nil {
				return err
			}
			prefix := ""
			if len(args) == 1 {
				prefix = args[0]
			}
			entries := docs.List(pages, locale, prefix)

			if globalsFrom(cmd).JSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetEscapeHTML(false)
				return enc.Encode(entries)
			}

			if len(entries) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Sin páginas bajo %q.\n", prefix)
				return nil
			}
			w := cmd.OutOrStdout()
			for _, e := range entries {
				fmt.Fprintf(w, "%s — %s\n", e.Path, e.Title)
			}
			return nil
		},
	}
	f.bind(c)
	return c
}

func newDocsGrepCmd() *cobra.Command {
	var f corpusFlags
	c := &cobra.Command{
		Use:   "grep <query>",
		Short: "Busca secciones de la documentación publicada (corpus en caché; para operaciones de la API usa `docs search`)",
		Args:  UsageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			pages, locale, err := loadDocsCorpus(cmd, f)
			if err != nil {
				return err
			}
			matches := docs.Grep(pages, locale, args[0])

			if globalsFrom(cmd).JSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetEscapeHTML(false)
				return enc.Encode(matches)
			}

			if len(matches) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Sin coincidencias para %q en la documentación.\n", args[0])
				return nil
			}
			w := cmd.OutOrStdout()
			for _, m := range matches {
				heading := ""
				if m.Section != "" {
					heading = " › " + m.Section
				}
				fmt.Fprintf(w, "%s — %s%s\n  %s\n", m.Path, m.Title, heading, m.Snippet)
			}
			return nil
		},
	}
	f.bind(c)
	return c
}

func newDocsGetCmd() *cobra.Command {
	var f corpusFlags
	c := &cobra.Command{
		Use:   "get <pagina>",
		Short: "Imprime el Markdown completo de una página de la documentación",
		Args:  UsageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			pages, locale, err := loadDocsCorpus(cmd, f)
			if err != nil {
				return err
			}
			page, err := docs.Get(pages, locale, args[0])
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if globalsFrom(cmd).JSON {
				enc := json.NewEncoder(w)
				enc.SetEscapeHTML(false)
				return enc.Encode(map[string]string{
					"path":     page.Path,
					"title":    page.Title,
					"markdown": page.Body,
				})
			}
			fmt.Fprintln(w, page.Body)
			return nil
		},
	}
	f.bind(c)
	return c
}
