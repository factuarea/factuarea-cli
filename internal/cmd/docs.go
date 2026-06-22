package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type docsMatch struct {
	Command string `json:"command"`
	Summary string `json:"summary"`
	Method  string `json:"method"`
	Path    string `json:"path"`
}

func newDocsCmd() *cobra.Command {
	docs := &cobra.Command{
		Use:   "docs",
		Short: "Referencia local de la API (sin conexión)",
		Args:  UsageArgs(cobra.NoArgs),
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	docs.AddCommand(newDocsSearchCmd())
	return docs
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
