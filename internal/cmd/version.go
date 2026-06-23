package cmd

import (
	"fmt"
	"os"

	"github.com/factuarea/factuarea-cli/internal/buildinfo"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/spec"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Muestra la versión del CLI",
		Args:  UsageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			g := globalsFrom(cmd)
			specHash := spec.Hash()[:12]
			if output.WantsJSON(g.JSON, os.Stdout) {
				return output.PrintJSON(cmd.OutOrStdout(), map[string]string{
					"version": buildinfo.Version,
					"commit":  buildinfo.Commit,
					"spec":    specHash,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "factuarea %s (commit %s, spec %s)\n",
				buildinfo.Version, buildinfo.Commit, specHash)
			return nil
		},
	}
}
