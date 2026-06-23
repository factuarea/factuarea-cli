package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/factuarea/factuarea-cli/internal/trigger"
	"github.com/spf13/cobra"
)

func newTriggerCmd() *cobra.Command {
	var overrides []string
	var list bool
	c := &cobra.Command{
		Use:   "trigger <evento>",
		Short: "Produce un evento real en sandbox (devloop)",
		Args:  UsageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			g := globalsFrom(cmd)
			asJSON := output.WantsJSON(g.JSON, os.Stdout)
			if list || len(args) == 0 {
				if asJSON {
					return output.PrintJSON(cmd.OutOrStdout(), trigger.Supported())
				}
				fmt.Fprintln(cmd.OutOrStdout(), strings.Join(trigger.Supported(), "\n"))
				return nil
			}
			ov, err := parseOverrides(overrides)
			if err != nil {
				return err
			}
			cc, err := newCLIContext(g, "")
			if err != nil {
				return err
			}
			if err := safety.RequireSandbox(cc.res.Environment); err != nil {
				return err
			}
			if err := trigger.Run(context.Background(), cc.client, args[0], ov); err != nil {
				return err
			}
			if asJSON {
				return output.PrintJSON(cmd.OutOrStdout(), map[string]any{
					"triggered": true,
					"event":     args[0],
				})
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "✓ Evento %q disparado en sandbox.\n", args[0])
			return nil
		},
	}
	c.Flags().StringArrayVar(&overrides, "override", nil, "sobreescribe campos del fixture (k=v)")
	c.Flags().BoolVar(&list, "list", false, "lista los eventos soportados")
	return c
}

func parseOverrides(raw []string) (map[string]string, error) {
	out := make(map[string]string, len(raw))
	for _, kv := range raw {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, apierr.Usagef("--override debe tener formato k=v (recibido %q)", kv)
		}
		out[k] = v
	}
	return out, nil
}
