package main

import (
	"fmt"
	"os"

	"github.com/factuarea/factuarea-cli/internal/cmd"
)

func main() {
	if err := cmd.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1) // TODO(task2): mapear exit codes con internal/exit.ForError
	}
}
