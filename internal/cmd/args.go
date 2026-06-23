package cmd

import (
	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/spf13/cobra"
)

func UsageArgs(v cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := v(cmd, args); err != nil {
			return apierr.Usagef("%s", translateCobraError(err.Error()))
		}
		return nil
	}
}
