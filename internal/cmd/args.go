package cmd

import (
	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/spf13/cobra"
)

// UsageArgs envuelve un validador de argumentos posicionales de cobra para que
// los errores de número/forma de argumentos se clasifiquen como uso incorrecto
// (exit code 2 / Usage), no como bug del CLI (exit 1). El generador de comandos
// debe envolver con esto todo cobra.ExactArgs/RangeArgs/etc.
func UsageArgs(v cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := v(cmd, args); err != nil {
			return &apierr.UsageError{Err: err}
		}
		return nil
	}
}
