package cmd

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
)

func newAPICmd() *cobra.Command {
	var data string
	cmd := &cobra.Command{
		Use:   "api <get|post|put|delete> <path>",
		Short: "Llamada genérica a la API v1 (escape hatch)",
		Args:  UsageArgs(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			g := globalsFrom(cmd)
			method := strings.ToUpper(args[0])
			path := args[1]
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			cc, err := newCLIContext(g, "")
			if err != nil {
				return err
			}
			// El guard --live se hereda para métodos mutadores.
			if isMutating(method) {
				if err := safety.RequireLive(cc.res.Environment, g.Live); err != nil {
					return err
				}
			}
			var body []byte
			if data != "" {
				body = []byte(data)
			}
			resp, err := cc.client.Do(context.Background(), method, path, body, nil)
			if err != nil {
				output.PrintError(cmd.ErrOrStderr(), err, cc.format)
				return &AlreadyReported{Err: err}
			}
			if g.Verbose && resp.RequestID != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "request_id: %s\n", resp.RequestID)
			}
			return output.PrintBody(cmd.OutOrStdout(), resp.Body, cc.format)
		},
	}
	cmd.Flags().StringVarP(&data, "data", "d", "", "cuerpo JSON de la petición")
	return cmd
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
