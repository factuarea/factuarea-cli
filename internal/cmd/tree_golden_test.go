package cmd

// Contraste congelado del ÁRBOL DE COMANDOS y comprobaciones del eje de empresa.
//
// Las dos cosas viven en el mismo fichero porque son el mismo cambio: el eje de
// empresa mueve la aridad y el uso de 12 comandos (los que declaran `{company}`
// en el path), y el contraste del árbol es lo que hace visible ese movimiento en
// una revisión en vez de dejarlo enterrado entre las 4.007 líneas de
// `resources_gen.go`. Cuando el árbol crezca lo suficiente, partir este fichero
// en dos es una decisión libre: no hay ninguna dependencia entre sus tests.

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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

// commandTreeManifest ejecuta `factuarea commands` por el MISMO camino que
// consume un agente —el comando del binario, no una reimplementación— y
// devuelve sus entradas ya decodificadas.
func commandTreeManifest(t *testing.T) []map[string]any {
	t.Helper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"commands"})
	if err := root.Execute(); err != nil {
		t.Fatalf("`factuarea commands`: %v", err)
	}
	var manifest []map[string]any
	if err := json.Unmarshal(out.Bytes(), &manifest); err != nil {
		t.Fatalf("el manifiesto no es JSON: %v", err)
	}
	return manifest
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

// Aritmética del contrato congelado de la ola (fuente:
// `dev_inventory_l16/1.8-recuentos.json`, medido el 2026-09-18):
//
//	470 operaciones fijadas el 2026-09-11
//	+144 de los tags NUEVOS del ERP
//	 +22 del ERP sobre tags preexistentes
//	 +18 de `Contacts`
//	=654 operaciones del contrato congelado (532 paths)
//
// El delta es +184, NO «las operaciones del ERP» (que son 166 = 144 + 22).
const (
	commandTreeOperationsBeforeWave   = 470
	commandTreeOperationsNewTags      = 144
	commandTreeOperationsExistingTags = 22
	commandTreeOperationsContacts     = 18
	commandTreeOperationsFrozen       = commandTreeOperationsBeforeWave +
		commandTreeOperationsNewTags + commandTreeOperationsExistingTags + commandTreeOperationsContacts
)

// TestCommandTreeCoversTheFrozenContract fija que el binario publica UN comando
// por operación del contrato congelado. Nace en ROJO: el spec embebido sigue en
// las 470 operaciones de la foto anterior y sólo lo re-fija la fase de
// regeneración, que es donde se observa su verde.
func TestCommandTreeCoversTheFrozenContract(t *testing.T) {
	if got := len(commandTreeManifest(t)); got != commandTreeOperationsFrozen {
		t.Fatalf("el manifiesto declara %d comandos y el contrato congelado %d operaciones "+
			"(%d previas +%d tags nuevos +%d tags preexistentes +%d Contacts): regenera el spec "+
			"embebido desde el contrato congelado, NUNCA desde producción.",
			got, commandTreeOperationsFrozen, commandTreeOperationsBeforeWave,
			commandTreeOperationsNewTags, commandTreeOperationsExistingTags, commandTreeOperationsContacts)
	}
}

// ---------------------------------------------------------------------------
// Eje de empresa: uso, aridad, composición del path y confirmación
// ---------------------------------------------------------------------------

// companyAxisCommand es una operación VIVA del contrato con DOS parámetros de
// ruta (`company` y `api_key`) y marcada como irreversible. No se inventa una
// operación de prueba: el riesgo que se fija es el de una operación real.
var companyAxisCommand = []string{"companies", "api-keys", "revoke"}

func findCommand(t *testing.T, path ...string) *cobra.Command {
	t.Helper()
	root := NewRootCmd()
	c, rest, err := root.Find(path)
	if err != nil || len(rest) > 0 {
		t.Fatalf("no encuentro el comando %v (resto %v): %v", path, rest, err)
	}
	return c
}

func TestCompanyPositionalIsOptionalInUsageAndArity(t *testing.T) {
	c := findCommand(t, companyAxisCommand...)

	if want := "revoke [company] <api_key>"; c.Use != want {
		t.Fatalf("Use = %q, want %q", c.Use, want)
	}
	if !strings.Contains(c.Long, "--company") {
		t.Errorf("la ayuda larga debe explicar la flag persistente --company; got:\n%s", c.Long)
	}

	for _, tc := range []struct {
		args []string
		ok   bool
		why  string
	}{
		{[]string{"key_7"}, true, "sólo el recurso: la empresa la aporta --company"},
		{[]string{"acme", "key_7"}, true, "empresa y recurso como posicionales (uso de siempre)"},
		{nil, false, "sin argumentos no hay recurso que revocar"},
		{[]string{"acme", "key_7", "sobra"}, false, "un posicional de más"},
	} {
		err := c.Args(c, tc.args)
		if tc.ok && err != nil {
			t.Errorf("args %v deberían aceptarse (%s): %v", tc.args, tc.why, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("args %v deberían rechazarse (%s)", tc.args, tc.why)
		}
	}
}

// TestSinglePositionalBodyFieldSurvivesTheArityRange cubre el caso que la
// ampliación de aridad puede romper en silencio: el atajo del campo ÚNICO de
// cuerpo, que también convierte la aridad exacta en un rango, pero por arriba.
func TestSinglePositionalBodyFieldSurvivesTheArityRange(t *testing.T) {
	c := findCommand(t, "products", "find-by-sku")
	if want := "find-by-sku [sku]"; c.Use != want {
		t.Fatalf("Use = %q, want %q", c.Use, want)
	}
	if err := c.Args(c, []string{"MI-SKU"}); err != nil {
		t.Fatalf("el atajo posicional debe seguir aceptándose: %v", err)
	}
	if err := c.Args(c, nil); err != nil {
		t.Fatalf("el atajo es OPCIONAL: sin posicional debe seguir valiendo --sku: %v", err)
	}
	if err := c.Args(c, []string{"A", "B"}); err == nil {
		t.Fatal("dos posicionales deben rechazarse: el atajo admite uno solo")
	}

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"prod_1"}}`))
	}))
	t.Cleanup(srv.Close)

	if _, err := runCmd(t, srv.URL, "products", "find-by-sku", "MI-SKU", "--json"); err != nil {
		t.Fatalf("find-by-sku con posicional: %v", err)
	}
	if body["sku"] != "MI-SKU" {
		t.Fatalf("el posicional debe mapear a `sku`: %v", body)
	}
}

// TestCompanyAxisBuildsTheSamePathInBothModes fija la composición del path con
// la empresa por posicional y con la empresa por flag persistente.
func TestCompanyAxisBuildsTheSamePathInBothModes(t *testing.T) {
	const wantPath = "/v1/companies/acme_co/api-keys/key_7"

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"posicional", []string{"companies", "api-keys", "revoke", "acme_co", "key_7"}},
		{"flag persistente", []string{"companies", "api-keys", "revoke", "key_7", "--company", "acme_co"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seen string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/account" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
					return
				}
				seen = r.Method + " " + r.URL.Path
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(srv.Close)

			args := append(append([]string{}, tc.args...), "--confirm", "key_7", "--json")
			if _, err := runCmd(t, srv.URL, args...); err != nil {
				t.Fatalf("ejecución: %v", err)
			}
			if want := "DELETE " + wantPath; seen != want {
				t.Fatalf("petición = %q, want %q", seen, want)
			}
		})
	}
}

// TestIrreversibleConfirmationNamesTheResourceNotTheCompany es el riesgo que el
// diseño ataca: hasta ahora la confirmación tomaba «el último posicional» y
// acertaba por casualidad. Con la flag, ese «último» cambia de sitio.
func TestIrreversibleConfirmationNamesTheResourceNotTheCompany(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"posicional", []string{"companies", "api-keys", "revoke", "acme_co", "key_7"}},
		{"flag persistente", []string{"companies", "api-keys", "revoke", "key_7", "--company", "acme_co"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/account" {
					t.Errorf("la red NO debe tocarse sin confirmación: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
			}))
			t.Cleanup(srv.Close)

			args := append(append([]string{}, tc.args...), "--no-input", "--skip-scope-check")
			_, err := runCmd(t, srv.URL, args...)
			if err == nil {
				t.Fatal("una operación irreversible sin --confirm debe fallar")
			}
			if !strings.Contains(err.Error(), "--confirm=key_7") {
				t.Fatalf("la confirmación debe nombrar el RECURSO (key_7): %v", err)
			}
			if strings.Contains(err.Error(), "acme_co") {
				t.Fatalf("la confirmación NO debe nombrar la empresa: %v", err)
			}
		})
	}
}

// TestCompanyOnlyOperationConfirmsWithTheCompany cubre el caso límite: cuando el
// eje es el ÚNICO parámetro de ruta, la empresa SÍ es el recurso.
func TestCompanyOnlyOperationConfirmsWithTheCompany(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/account" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
			return
		}
		seen = r.Method + " " + r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	out, err := runCmd(t, srv.URL, "companies", "delete", "--company", "acme_co", "--skip-scope-check", "--json")
	if err != nil {
		t.Fatalf("ejecución: %v", err)
	}
	if want := "DELETE /v1/companies/acme_co"; seen != want {
		t.Fatalf("petición = %q, want %q", seen, want)
	}
	if !strings.Contains(out, `"id": "acme_co"`) && !strings.Contains(out, `"id":"acme_co"`) {
		t.Fatalf("la confirmación de borrado debe nombrar la empresa: %s", out)
	}
}

func TestCompanyByBothWaysIsAUsageError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account" {
			t.Errorf("la red NO debe tocarse: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
	}))
	t.Cleanup(srv.Close)

	_, err := runCmd(t, srv.URL, "companies", "api-keys", "revoke", "acme_co", "key_7",
		"--company", "otra_co", "--confirm", "key_7", "--skip-scope-check")
	if err == nil {
		t.Fatal("aportar la empresa por las dos vías debe ser error de uso")
	}
	if !strings.Contains(err.Error(), "--company") || !strings.Contains(err.Error(), "acme_co") {
		t.Fatalf("el mensaje debe nombrar el conflicto y los dos valores: %v", err)
	}
}

func TestCompanyMissingFromBothWaysIsAUsageError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/account" {
			t.Errorf("la red NO debe tocarse: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
	}))
	t.Cleanup(srv.Close)

	_, err := runCmd(t, srv.URL, "companies", "api-keys", "revoke", "key_7",
		"--confirm", "key_7", "--skip-scope-check")
	if err == nil {
		t.Fatal("sin empresa por ninguna de las dos vías debe fallar antes de la red")
	}
	if !strings.Contains(err.Error(), "--company") {
		t.Fatalf("el mensaje debe ofrecer la flag persistente: %v", err)
	}
}

// TestManifestDeclaresTheCompanyAxisOnlyWhereItApplies comprueba que el
// manifiesto refleja la opcionalidad del posicional de empresa exactamente en
// las operaciones que declaran `{company}` y en ninguna otra.
func TestManifestDeclaresTheCompanyAxisOnlyWhereItApplies(t *testing.T) {
	withCompanyParam := map[string]bool{}
	for _, op := range generatedOps() {
		if op.companyPathParamIndex() >= 0 {
			withCompanyParam[commandPath(op)] = true
		}
	}
	if len(withCompanyParam) == 0 {
		t.Fatal("ninguna operación declara {company}: el eje no se está midiendo")
	}

	declared := map[string]bool{}
	for _, entry := range commandTreeManifest(t) {
		name, _ := entry["command"].(string)
		optional, ok := entry["optional_args"].(map[string]any)
		if !ok {
			continue
		}
		if flagName, ok := optional["company"].(string); !ok || flagName != "--company" {
			t.Errorf("%s declara optional_args inesperado: %v", name, optional)
			continue
		}
		if len(optional) != 1 {
			t.Errorf("%s declara más argumentos opcionales de los que hay: %v", name, optional)
		}
		declared[name] = true
	}

	for name := range withCompanyParam {
		if !declared[name] {
			t.Errorf("%s declara {company} en el path pero el manifiesto no lo marca como opcional", name)
		}
	}
	for name := range declared {
		if !withCompanyParam[name] {
			t.Errorf("%s se marca como opcional en el manifiesto sin declarar {company} en el path", name)
		}
	}
}
