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
	// Comprobar `nonConforming` ANTES de escribir: un operationId sin
	// namespacear ya no se avisa y se descarta en silencio, PARA sin tocar
	// el fichero generado. Corregir el spec en el backend, nunca aquí.
	if len(nonConforming) > 0 {
		fmt.Fprintf(os.Stderr, "ERROR: %d operationId no namespaceados: %v; corrige en backend\n", len(nonConforming), nonConforming)
		os.Exit(1)
	}
	if err := os.WriteFile(outPath, src, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error escribiendo %s: %v\n", outPath, err)
		os.Exit(1)
	}
}
