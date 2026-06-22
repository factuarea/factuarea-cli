package cmd

import (
	"context"
	"fmt"
	"strings"

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
			if list || len(args) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), strings.Join(trigger.Supported(), "\n"))
				return nil
			}
			g := globalsFrom(cmd)
			cc, err := newCLIContext(g, "")
			if err != nil {
				return err
			}
			if err := safety.RequireSandbox(cc.res.Environment); err != nil {
				return err
			}
			if err := trigger.Run(context.Background(), cc.client, args[0], parseOverrides(overrides)); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "✓ Evento %q disparado en sandbox.\n", args[0])
			return nil
		},
	}
	c.Flags().StringArrayVar(&overrides, "override", nil, "sobreescribe campos del fixture (k=v)")
	c.Flags().BoolVar(&list, "list", false, "lista los eventos soportados")
	return c
}

func parseOverrides(raw []string) map[string]string {
	out := make(map[string]string, len(raw))
	for _, kv := range raw {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		out[k] = v
	}
	return out
}
