package cmd

// Contraste congelado del ÁRBOL DE COMANDOS: hace visible en una revisión
// cualquier comando que aparece, desaparece o cambia de forma, en vez de dejarlo
// enterrado entre las miles de líneas de `resources_gen.go`.

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Contraste del árbol de comandos contra la referencia versionada
// ---------------------------------------------------------------------------

const commandTreeGoldenPath = "testdata/command_tree.json"

// updateCommandTree regenera la referencia. Se pasa SOLO cuando el árbol ha
// cambiado a propósito (una regeneración del contrato), nunca para «arreglar»
// un rojo: el rojo es el aviso de que un comando apareció, desapareció o cambió
// de forma.
var updateCommandTree = flag.Bool("update-command-tree", false,
	"regenera "+commandTreeGoldenPath+" desde el manifiesto del binario")

// frozenManifestFields son los campos del manifiesto que el contraste congela:
// el comando, sus argumentos posicionales (con los que la flag persistente
// puede aportar), sus opciones con tipo y las marcas de mutación, binario,
// paginación e irreversibilidad. Es la forma del comando, que es lo que un
// agente y un humano consumen.
var frozenManifestFields = []string{
	"command",
	"operation_id",
	"args",
	"optional_args",
	"flags",
	"mutating",
	"binary",
	"paginated",
	"irreversible",
}

// excludedManifestFields son los campos que el contraste NO congela, cada uno
// con su razón escrita. La lista es exhaustiva a propósito: si el manifiesto
// gana un campo nuevo, `TestCommandTreeClassifiesEveryManifestField` falla
// hasta que alguien lo clasifique. Así el congelado no se queda callado ante
// una superficie nueva.
var excludedManifestFields = map[string]string{
	"full_replace": "DEUDA DECLARADA: lo decide `isUpdate()` por el método HTTP, así que MIENTE " +
		"para los comandos de edición PARCIAL (PATCH), que son la mayoría. Congelar un dato que " +
		"se sabe falso convertiría la deuda en contrato verificado; el sitio de arreglarlo es el " +
		"manifiesto, no esta referencia.",
	"summary": "Es la prosa del contrato: cambia con cada reescritura de un `summary` en el " +
		"backend sin que el árbol de comandos se mueva ni un milímetro.",
	"example": "Sale del ejemplo del cuerpo declarado en el contrato; su cobertura la miden los " +
		"verificadores de ejemplos del portal, no este contraste.",
	"body_fields": "El cuerpo tipado tiene sus propios tests (`bodyflags_test.go`) y su tamaño " +
		"ahogaría el diff del árbol: una referencia que nadie puede leer no señala nada.",
	"body_has_object_array": "Deriva de `body_fields`, que ya queda fuera.",
	"deprecated":            "Lo declara el contrato y se reconcilia contra él, no contra una copia local.",
	"required_scope": "Lo declara el contrato (`x-required-scope`) y la reconciliación de la ola lo " +
		"cruza contra el inventario congelado de operaciones por tag: congelarlo aquí duplicaría el " +
		"criterio en dos sitios que se desincronizarían.",
}

// freezeCommandTree proyecta el manifiesto sobre los campos congelados. El
// resultado es JSON indentado y estable: las entradas conservan el orden del
// manifiesto (que es el del contrato: path y luego método) y las claves salen
// ordenadas por `encoding/json`.
func freezeCommandTree(t *testing.T, manifest []map[string]any) []byte {
	t.Helper()
	frozen := make([]map[string]any, 0, len(manifest))
	for _, entry := range manifest {
		row := map[string]any{}
		for _, field := range frozenManifestFields {
			if v, ok := entry[field]; ok {
				row[field] = v
			}
		}
		frozen = append(frozen, row)
	}
	raw, err := json.MarshalIndent(frozen, "", "  ")
	if err != nil {
		t.Fatalf("serializando el árbol: %v", err)
	}
	return append(raw, '\n')
}

// TestCommandTreeMatchesGolden contrasta el árbol completo con la referencia
// versionada de `testdata/`.
//
// La referencia NO se teclea: se PRODUCE desde el manifiesto del propio binario
// con
//
//	go test ./internal/cmd/ -run TestCommandTreeMatchesGolden -update-command-tree
//
// y se commitea tal cual. Editarla a mano es exactamente el fallo que este test
// existe para cazar, porque una referencia retocada declara verde un comando
// que el binario ya no publica.
func TestCommandTreeMatchesGolden(t *testing.T) {
	got := freezeCommandTree(t, commandTreeManifest(t))

	if *updateCommandTree {
		if err := os.MkdirAll(filepath.Dir(commandTreeGoldenPath), 0o755); err != nil {
			t.Fatalf("creando %s: %v", filepath.Dir(commandTreeGoldenPath), err)
		}
		if err := os.WriteFile(commandTreeGoldenPath, got, 0o644); err != nil {
			t.Fatalf("escribiendo %s: %v", commandTreeGoldenPath, err)
		}
		t.Logf("referencia regenerada en %s (%d comandos)", commandTreeGoldenPath, bytes.Count(got, []byte(`"command":`)))
		return
	}

	want, err := os.ReadFile(commandTreeGoldenPath)
	if err != nil {
		t.Fatalf("no encuentro la referencia del árbol (%s): %v\n"+
			"Prodúcela con:\n  go test ./internal/cmd/ -run TestCommandTreeMatchesGolden -update-command-tree",
			commandTreeGoldenPath, err)
	}
	if bytes.Equal(got, want) {
		return
	}
	t.Errorf("el árbol de comandos NO coincide con %s.\n%s\n"+
		"Si el cambio es intencionado (regeneración del contrato), regenera la referencia con:\n"+
		"  go test ./internal/cmd/ -run TestCommandTreeMatchesGolden -update-command-tree",
		commandTreeGoldenPath, describeCommandTreeDiff(t, want, got))
}

// describeCommandTreeDiff resume la diferencia en términos de COMANDOS —cuáles
// aparecen, cuáles desaparecen y cuáles cambian de forma— en vez de volcar dos
// ficheros de miles de líneas.
func describeCommandTreeDiff(t *testing.T, want, got []byte) string {
	t.Helper()
	index := func(raw []byte) map[string]string {
		var rows []map[string]any
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil
		}
		out := map[string]string{}
		for _, row := range rows {
			name, _ := row["command"].(string)
			shape, _ := json.Marshal(row)
			out[name] = string(shape)
		}
		return out
	}
	before, after := index(want), index(got)
	if before == nil || after == nil {
		return "  (no pude decodificar una de las dos partes; compara los ficheros a mano)"
	}
	var added, removed, changed []string
	for name, shape := range after {
		prev, ok := before[name]
		switch {
		case !ok:
			added = append(added, name)
		case prev != shape:
			changed = append(changed, name)
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			removed = append(removed, name)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)
	var b strings.Builder
	for _, block := range []struct {
		label string
		names []string
	}{
		{"comandos NUEVOS", added},
		{"comandos DESAPARECIDOS", removed},
		{"comandos con la forma CAMBIADA", changed},
	} {
		if len(block.names) == 0 {
			continue
		}
		fmt.Fprintf(&b, "  %s (%d):\n", block.label, len(block.names))
		for _, n := range block.names {
			fmt.Fprintf(&b, "    - %s\n", n)
		}
	}
	if b.Len() == 0 {
		return "  (mismo conjunto de comandos con la misma forma: la diferencia está en el formato del fichero)"
	}
	return strings.TrimRight(b.String(), "\n")
}

// TestCommandTreeClassifiesEveryManifestField impide que un campo nuevo del
// manifiesto entre en el congelado —o se quede fuera de él— sin que nadie lo
// decida. No es una lista que se valide a sí misma: los campos se MIDEN sobre
// la salida real del binario.
func TestCommandTreeClassifiesEveryManifestField(t *testing.T) {
	frozen := map[string]bool{}
	for _, f := range frozenManifestFields {
		frozen[f] = true
	}
	seen := map[string]bool{}
	for _, entry := range commandTreeManifest(t) {
		for field := range entry {
			seen[field] = true
		}
	}
	var unclassified []string
	for field := range seen {
		if frozen[field] {
			continue
		}
		if _, excluded := excludedManifestFields[field]; excluded {
			continue
		}
		unclassified = append(unclassified, field)
	}
	sort.Strings(unclassified)
	if len(unclassified) > 0 {
		t.Fatalf("campos del manifiesto sin clasificar: %v.\n"+
			"Decide si entran en `frozenManifestFields` o en `excludedManifestFields` CON SU RAZÓN; "+
			"dejarlos fuera en silencio es no congelar nada.", unclassified)
	}
	for _, f := range frozenManifestFields {
		if !seen[f] && f != "optional_args" {
			t.Errorf("el campo congelado %q ya no aparece en ninguna entrada del manifiesto", f)
		}
	}
}

// commandTreeOperationsFrozen es el número de operaciones del contrato público
// congelado (830 operaciones en 666 paths); el binario publica un comando por
// operación.
const commandTreeOperationsFrozen = 830

// TestCommandTreeCoversTheFrozenContract fija que el binario publica UN comando
// por operación del contrato congelado.
func TestCommandTreeCoversTheFrozenContract(t *testing.T) {
	if got := len(commandTreeManifest(t)); got != commandTreeOperationsFrozen {
		t.Fatalf("el manifiesto declara %d comandos y el contrato congelado %d operaciones: "+
			"regenera el spec embebido desde el contrato congelado, NUNCA desde producción.",
			got, commandTreeOperationsFrozen)
	}
}
