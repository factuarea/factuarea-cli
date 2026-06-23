package cmd

import (
	"fmt"

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

func groupArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	msg := fmt.Sprintf(`unknown command "%s" for "%s"`, args[0], cmd.CommandPath())
	if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
		msg += "\n\nDid you mean this?\n"
		for _, s := range suggestions {
			msg += "\t" + s + "\n"
		}
	}
	return apierr.Usagef("%s", translateCobraError(msg))
}
