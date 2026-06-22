//go:build ignore

// Binario del generador. Ejecuta gen.Generate(), escribe la tabla en
// internal/cmd/resources_gen.go (ruta relativa a la raíz del repo, así que
// córrelo desde la raíz: `go run internal/gen/main.go`) y logea a stderr los
// operationIds NO conformes que se omitieron de la tabla. NUNCA los silencia.
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
