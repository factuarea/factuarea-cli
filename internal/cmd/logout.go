package cmd

import (
	"fmt"
	"os"

	"github.com/factuarea/factuarea-cli/internal/config"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/spf13/cobra"
)

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Borra las credenciales del perfil activo",
		Args:  UsageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			g := globalsFrom(cmd)
			profile := g.Profile
			if profile == "" {
				profile = "default"
			}
			store, _ := config.NewStore()
			if err := store.DeleteKey(profile); err != nil {
				return err
			}
			if output.WantsJSON(g.JSON, os.Stdout) {
				return output.PrintJSON(cmd.OutOrStdout(), map[string]any{
					"logged_out": true,
					"profile":    profile,
				})
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "✓ Sesión cerrada (perfil %q).\n", profile)
			return nil
		},
	}
}
