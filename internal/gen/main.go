//go:build ignore

package main

import (
	"fmt"
	"os"

	"github.com/factuarea/factuarea-cli/internal/gen"
)

const outPath = "internal/cmd/resources_gen.go"

func main() {
	src, nonConforming, err := gen.Generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generando %s: %v\n", outPath, err)
		os.Exit(1)
	}
	if err := os.WriteFile(outPath, src, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error escribiendo %s: %v\n", outPath, err)
		os.Exit(1)
	}
	if len(nonConforming) > 0 {
		fmt.Fprintf(os.Stderr, "AVISO: %d operationId no namespaceados, omitidos del CLI: %v; corrige en backend\n", len(nonConforming), nonConforming)
	}
}
