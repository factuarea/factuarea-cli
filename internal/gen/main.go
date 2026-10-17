//go:build ignore

package main

import (
	"fmt"
	"os"

	"github.com/factuarea/factuarea-cli/internal/gen"
	"github.com/factuarea/factuarea-cli/internal/spec"
)

const outPath = "internal/cmd/resources_gen.go"

func main() {
	src, nonConforming, err := gen.Generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generando %s: %v\n", outPath, err)
		os.Exit(1)
	}
	// El fichero se escribe SIEMPRE, incluso cuando hay identificadores no
	// canónicos: el diff de lo generado es la prueba de qué superficie falta y
	// tiene que quedar inspeccionable. El fallo viene DESPUÉS de escribirlo.
	if err := os.WriteFile(outPath, src, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "error escribiendo %s: %v\n", outPath, err)
		os.Exit(1)
	}
	if len(nonConforming) > 0 {
		reportNonConforming(nonConforming)
		os.Exit(1)
	}
}

// reportNonConforming imprime el fallo duro del generador: una operación sin el
// prefijo canónico desaparecería del binario, de las completions, de las
// manpages y del manifiesto sin romper nada, así que el proceso termina con
// código 1. El gate corre ANTES de regenerar porque después el identificador
// que la delata ya no existe en el árbol.
func reportNonConforming(ids []string) {
	fmt.Fprintf(os.Stderr, "ERROR: %d operationId sin el prefijo canónico %q; NO se han generado comandos para ellos:\n",
		len(ids), spec.OperationIDPrefix)
	for _, id := range ids {
		fmt.Fprintf(os.Stderr, "  - %s\n", id)
	}
	fmt.Fprintf(os.Stderr, `
El arreglo va en el BACKEND, no aquí: nombra la ruta con
->name('%s<recurso>.<accion>') en su fichero de rutas de la API pública v1 y
vuelve a exportar el contrato. Un identificador canónico tiene AL MENOS dos
segmentos tras el prefijo (recurso y acción); los recursos anidados añaden
segmentos intermedios (por ejemplo '%sproducts.variants.list').

NUNCA lo tapes dando de alta la operación en el mapa `+"`overrides`"+` de
internal/cmd/register.go: ese mapa corrige la FORMA de un comando que ya
existe, no inventa el que el contrato no nombró, y usarlo aquí dejaría el CLI
publicando una superficie que ni el portal ni los SDK conocen.
`, spec.OperationIDPrefix, spec.OperationIDPrefix)
}
