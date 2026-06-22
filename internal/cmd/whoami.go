package cmd

import (
	"context"
	"fmt"

	"github.com/factuarea/factuarea-cli/internal/config"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/spf13/cobra"
)

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Muestra la cuenta autenticada y el entorno (test/live)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			g := globalsFrom(cmd)
			cc, err := newCLIContext(g, "")
			if err != nil {
				return err
			}
			resp, err := cc.client.Do(context.Background(), "GET", "/v1/account", nil, nil)
			if err != nil {
				output.PrintError(cmd.ErrOrStderr(), err, cc.format)
				return &AlreadyReported{Err: err}
			}
			if cc.format == output.Human && !g.Quiet {
				fmt.Fprintf(cmd.ErrOrStderr(), "[%s] perfil %q (key %s)\n",
					upper(cc.res.Environment), cc.res.Profile, config.RedactKey(cc.res.APIKey))
			}
			return output.PrintBody(cmd.OutOrStdout(), resp.Body, cc.format)
		},
	}
}

func upper(s string) string {
	switch s {
	case "test":
		return "TEST"
	case "live":
		return "LIVE"
	default:
		return "DESCONOCIDO"
	}
}
