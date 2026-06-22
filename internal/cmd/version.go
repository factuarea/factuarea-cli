package cmd

import (
	"fmt"

	"github.com/factuarea/factuarea-cli/internal/buildinfo"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Muestra la versión del CLI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(cmd.OutOrStdout(), "factuarea %s (commit %s, spec %s)\n",
				buildinfo.Version, buildinfo.Commit, buildinfo.SpecHash)
			return nil
		},
	}
}
