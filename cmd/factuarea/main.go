package main

import (
	"fmt"
	"os"

	"github.com/factuarea/factuarea-cli/internal/cmd"
	"github.com/factuarea/factuarea-cli/internal/exit"
)

func main() {
	if err := cmd.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exit.ForError(err)) // AlreadyReported lo maneja Task 8
	}
}
