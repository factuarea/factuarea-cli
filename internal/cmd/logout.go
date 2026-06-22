package cmd

import (
	"fmt"

	"github.com/factuarea/factuarea-cli/internal/config"
	"github.com/spf13/cobra"
)

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Borra las credenciales del perfil activo",
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
			fmt.Fprintf(cmd.ErrOrStderr(), "✓ Sesión cerrada (perfil %q).\n", profile)
			return nil
		},
	}
}
