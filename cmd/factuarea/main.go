package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/factuarea/factuarea-cli/internal/cmd"
	"github.com/factuarea/factuarea-cli/internal/exit"
)

func main() {
	root := cmd.NewRootCmd()
	if err := root.Execute(); err != nil {
		var silent *cmd.AlreadyReported
		if !errors.As(err, &silent) {
			fmt.Fprintln(os.Stderr, err.Error())
		}
		os.Exit(exit.ForError(err))
	}
}
