package main

import (
	"errors"
	"os"

	"github.com/factuarea/factuarea-cli/internal/cmd"
	"github.com/factuarea/factuarea-cli/internal/exit"
	"github.com/factuarea/factuarea-cli/internal/output"
)

func main() {
	root := cmd.NewRootCmd()
	if err := root.Execute(); err != nil {
		err = cmd.FinalizeError(err)
		var silent *cmd.AlreadyReported
		if !errors.As(err, &silent) {
			output.PrintError(os.Stderr, err, errorFormat())
		}
		os.Exit(exit.ForError(err))
	}
}

func errorFormat() output.Format {
	for _, a := range os.Args[1:] {
		if a == "--json" {
			return output.JSON
		}
		if a == "--" {
			break
		}
	}
	return output.ResolveErrorFormat(false, os.Stderr)
}
