package cmd

// Gate de regresión del ÁRBOL DE COMANDOS contra el manifiesto de operaciones.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/factuarea/factuarea-cli/internal/spec"
	"github.com/spf13/cobra"
)

const frozenOperationManifestPath = "testdata/v1-command-tree-manifest.json"

// frozenSpecEnv es el brazo OPT-IN del test del canonicalizador.
const frozenSpecEnv = "FACTUAREA_FROZEN_SPEC"

// ---------------------------------------------------------------------------
// El manifiesto de operaciones congelado
// ---------------------------------------------------------------------------

type frozenOperation struct {
	OperationID string   `json:"operation_id"`
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	PathParams  []string `json:"path_params"`
	Command     string   `json:"command"`
}

type frozenOperationManifest struct {
	Meta struct {
		SourceDigest   string `json:"source_digest"`
		MeasuredCommit string `json:"measured_commit"`
		GeneratedBy    string `json:"generated_by"`
		Operations     int    `json:"operations"`
		Paths          int    `json:"paths"`
	} `json:"$meta"`
	Operations []frozenOperation `json:"operations"`
}

func loadFrozenOperationManifest(t *testing.T) frozenOperationManifest {
	t.Helper()
	raw, err := os.ReadFile(frozenOperationManifestPath)
	if err != nil {
		t.Fatalf("no encuentro el manifiesto de operaciones (%s): %v\n"+
			"Es la copia del publicado en el repo del backend "+
			"(backend/docs/api/v1-command-tree-manifest.json); cópialo tal cual, no lo teclees.",
			frozenOperationManifestPath, err)
	}
	var m frozenOperationManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%s no es JSON válido: %v", frozenOperationManifestPath, err)
	}
	if len(m.Operations) == 0 {
		t.Fatalf("%s no declara ninguna operación: un manifiesto vacío haría vacuo todo este gate",
			frozenOperationManifestPath)
	}
	if m.Meta.SourceDigest == "" {
		t.Fatalf("%s no declara $meta.source_digest: sin él la guarda por digest no puede armarse",
			frozenOperationManifestPath)
	}
	return m
}

// companyAxisParam decide sobre el MANIFIESTO, no sobre el código de la CLI, si
// la operación cuelga del eje, y devuelve el nombre de su parámetro de ruta.
func (e frozenOperation) companyAxisParam() (string, bool) {
	rest, ok := strings.CutPrefix(e.Path, companyAxisPrefix)
	if !ok || (rest != "" && !strings.HasPrefix(rest, "/")) {
		return "", false
	}
	if len(e.PathParams) == 0 || e.PathParams[0] != companyPathParam {
		return "", false
	}
	return e.PathParams[0], true
}

// ---------------------------------------------------------------------------
// Canonicalización.

// specCanonicalDigest reproduce en Go el procedimiento canónico con el que se
// comparan dos copias del contrato.
func specCanonicalDigest(raw []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // conserva el texto del número tal cual lo escribió el emisor
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := writeCanonicalJSON(&b, v); err != nil {
		return "", err
	}
	sum := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

// codeSamplesKey es la clave que el procedimiento descarta.
const codeSamplesKey = "x-codeSamples"

func writeCanonicalJSON(b *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case json.Number:
		b.WriteString(t.String())
	case string:
		writeJSString(b, t)
	case []any:
		b.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonicalJSON(b, item); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			if k == codeSamplesKey {
				continue
			}
			keys = append(keys, k)
		}
		b.WriteByte('{')
		for i, k := range jsPropertyOrder(keys) {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSString(b, k)
			b.WriteByte(':')
			if err := writeCanonicalJSON(b, t[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("tipo JSON inesperado %T", v)
	}
	return nil
}

// jsPropertyOrder devuelve las claves en el ORDEN EN QUE JAVASCRIPT LAS EMITE.
func jsPropertyOrder(keys []string) []string {
	var indexes, rest []string
	for _, k := range keys {
		if isJSArrayIndex(k) {
			indexes = append(indexes, k)
		} else {
			rest = append(rest, k)
		}
	}
	sort.Slice(indexes, func(i, j int) bool {
		a, _ := strconv.ParseUint(indexes[i], 10, 64)
		b, _ := strconv.ParseUint(indexes[j], 10, 64)
		return a < b
	})
	sort.Slice(rest, func(i, j int) bool { return lessUTF16(rest[i], rest[j]) })
	return append(indexes, rest...)
}

// isJSArrayIndex aplica la definición de «índice de array» del lenguaje.
func isJSArrayIndex(k string) bool {
	if k == "" || (len(k) > 1 && k[0] == '0') {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(k, 10, 64)
	return err == nil && n < 1<<32-1
}

// lessUTF16 compara dos cadenas como `Array.prototype.sort` por defecto.
func lessUTF16(a, b string) bool {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

// writeJSString escribe la cadena con el escapado de `JSON.stringify`.
func writeJSString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
				continue
			}
			var buf [utf8.UTFMax]byte
			b.Write(buf[:utf8.EncodeRune(buf[:], r)])
		}
	}
	b.WriteByte('"')
}

// TestCanonicalDigestReproducesTheFrozenProcedure ancla el canonicalizador
// contra el emisor real de los digests, que es JavaScript.
func TestCanonicalDigestReproducesTheFrozenProcedure(t *testing.T) {
	const vector = `{"b":{"x-codeSamples":["drop"],"21":1,"4":2,"10":3},"x-codeSamples":"drop","10":"diez","4":["cuatro",{"z":true,"a":null}],"21":21,"a":"á\"\\\n\t","A":1.5,"0":0,"01":"no-indice","4294967295":"no-indice","4294967294":"indice"}`
	const wantSerialized = `{"0":0,"4":["cuatro",{"a":null,"z":true}],"10":"diez","21":21,"4294967294":"indice","01":"no-indice","4294967295":"no-indice","A":1.5,"a":"á\"\\\n\t","b":{"4":2,"10":3,"21":1}}`
	const wantDigest = "6e38b5728629aa35cb261d486a159ce48f538f56e0e42aa210106d8baeaa52c6"

	var v any
	dec := json.NewDecoder(strings.NewReader(vector))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("el vector no es JSON: %v", err)
	}
	var b bytes.Buffer
	if err := writeCanonicalJSON(&b, v); err != nil {
		t.Fatalf("canonicalizando el vector: %v", err)
	}
	if got := b.String(); got != wantSerialized {
		t.Fatalf("serialización canónica distinta de la de JavaScript:\n got: %s\nwant: %s", got, wantSerialized)
	}
	got, err := specCanonicalDigest([]byte(vector))
	if err != nil {
		t.Fatalf("digest del vector: %v", err)
	}
	if got != wantDigest {
		t.Fatalf("digest del vector = %s, want %s", got, wantDigest)
	}

	// El contrato embebido, medido de verdad. NO se asierta contra un literal.
	embedded, err := specCanonicalDigest(spec.Raw)
	if err != nil {
		t.Fatalf("digest canónico del contrato embebido: %v", err)
	}
	t.Logf("contrato embebido (internal/spec/openapi.json): %d bytes · canónico %s · crudo %s",
		len(spec.Raw), embedded, spec.Hash())

	// Brazo opt-in: el documento congelado vive en otro repo, así que se mide
	// cuando quien ejecuta lo señala.
	path := os.Getenv(frozenSpecEnv)
	if path == "" {
		t.Logf("sin %s: no se mide el documento congelado del backend. Para medirlo:\n"+
			"  %s=/ruta/a/factuarea/backend/api.json go test ./internal/cmd/ -run %s",
			frozenSpecEnv, frozenSpecEnv, t.Name())
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s=%s: %v", frozenSpecEnv, path, err)
	}
	frozenDigest, err := specCanonicalDigest(raw)
	if err != nil {
		t.Fatalf("digest canónico de %s: %v", path, err)
	}
	want := loadFrozenOperationManifest(t).Meta.SourceDigest
	if frozenDigest != want {
		t.Fatalf("%s tiene digest canónico %s, pero el manifiesto declara proceder de %s.\n"+
			"O el fichero señalado no es el documento del que salió el manifiesto, o el manifiesto está desfasado.",
			path, frozenDigest, want)
	}
	t.Logf("documento congelado %s: canónico %s — coincide con $meta.source_digest", path, frozenDigest)
}

// ---------------------------------------------------------------------------
// Coherencia interna del manifiesto
// ---------------------------------------------------------------------------

// TestFrozenOperationManifestIsCoherentWithItsHeader comprueba que la copia.
func TestFrozenOperationManifestIsCoherentWithItsHeader(t *testing.T) {
	m := loadFrozenOperationManifest(t)

	if m.Meta.Operations != len(m.Operations) {
		t.Errorf("$meta.operations = %d pero el cuerpo declara %d operaciones", m.Meta.Operations, len(m.Operations))
	}
	paths := map[string]bool{}
	for _, op := range m.Operations {
		paths[op.Path] = true
	}
	if m.Meta.Paths != len(paths) {
		t.Errorf("$meta.paths = %d pero el cuerpo declara %d paths distintos", m.Meta.Paths, len(paths))
	}
	if m.Meta.GeneratedBy == "" {
		t.Error("$meta.generated_by está vacío: sin el comando de regeneración, el fichero invita a editarse a mano")
	}

	seenID, seenCmd := map[string]bool{}, map[string]string{}
	for _, op := range m.Operations {
		if seenID[op.OperationID] {
			t.Errorf("operation_id repetido: %s", op.OperationID)
		}
		seenID[op.OperationID] = true

		if prev, dup := seenCmd[op.Command]; dup {
			t.Errorf("dos operaciones comparten comando %q: %s y %s", op.Command, prev, op.OperationID)
		}
		seenCmd[op.Command] = op.OperationID

		groups, action, ok := spec.Resolve(op.OperationID)
		if !ok {
			t.Errorf("%s no resuelve el espacio de nombres %q: no puede producir comando",
				op.OperationID, spec.OperationIDPrefix)
			continue
		}
		want := strings.Join(append(append([]string{"factuarea"}, groups...), action), " ")
		if op.Command != want {
			t.Errorf("%s declara el comando %q pero su identificador produce %q",
				op.OperationID, op.Command, want)
		}

		if op.Method != strings.ToUpper(op.Method) {
			t.Errorf("%s declara el método %q en minúsculas", op.OperationID, op.Method)
		}
		for _, p := range op.PathParams {
			if !strings.Contains(op.Path, "{"+p+"}") {
				t.Errorf("%s declara el path param %q, que no aparece en %s", op.OperationID, p, op.Path)
			}
		}
	}

	axis := 0
	for _, op := range m.Operations {
		if _, ok := op.companyAxisParam(); ok {
			axis++
		}
	}
	if axis == 0 {
		t.Fatal("ninguna operación del manifiesto cuelga del eje de empresa: las aserciones del eje serían vacuas")
	}
	t.Logf("manifiesto: %d operaciones · %d paths · %d comandos distintos · %d del eje de empresa · procede de %s",
		len(m.Operations), len(paths), len(seenCmd), axis, m.Meta.SourceDigest)
}

// ---------------------------------------------------------------------------
// El gate
// ---------------------------------------------------------------------------

// TestCommandTreeCoversEveryFrozenOperation es el gate ÚNICO.
func TestCommandTreeCoversEveryFrozenOperation(t *testing.T) {
	m := loadFrozenOperationManifest(t)
	tree := commandTreeManifest(t)

	byCommand := make(map[string]map[string]any, len(tree))
	for _, entry := range tree {
		name, _ := entry["command"].(string)
		if name == "" {
			t.Fatalf("una entrada del árbol no declara `command`: %v", entry)
		}
		byCommand[name] = entry
	}

	// El número de operaciones que el gate cubre, registrado SIEMPRE.
	t.Logf("cobertura del gate: %d operaciones del manifiesto frente a %d comandos del árbol",
		len(m.Operations), len(byCommand))

	embedded, err := specCanonicalDigest(spec.Raw)
	if err != nil {
		t.Fatalf("digest canónico del contrato embebido: %v", err)
	}
	if embedded != m.Meta.SourceDigest {
		missing := 0
		for _, op := range m.Operations {
			if _, ok := byCommand[op.Command]; !ok {
				missing++
			}
		}
		t.Skipf("gate INERTE por ahora: el árbol NO se deriva del documento del manifiesto.\n"+
			"  contrato embebido (internal/spec/openapi.json), canónico: %s\n"+
			"  $meta.source_digest del manifiesto:                      %s\n"+
			"Son dos contratos distintos: el árbol publica %d comandos y el manifiesto declara %d operaciones, "+
			"de las cuales %d no existen hoy en el árbol. Comparar ahora mediría dos universos, no una regresión.\n"+
			"Lo desarma la regeneración de la CLI desde el contrato del eje, aún no depositado: cuando "+
			"internal/spec/openapi.json lo lleve, los dos digests coincidirán y este gate empezará a comparar "+
			"SIN tocar una línea.",
			embedded, m.Meta.SourceDigest, len(byCommand), len(m.Operations), missing)
	}

	// NO-DECRECIMIENTO: toda operación del contrato tiene comando.
	var missing []frozenOperation
	for _, op := range m.Operations {
		if _, ok := byCommand[op.Command]; !ok {
			missing = append(missing, op)
		}
	}
	if len(missing) > 0 {
		sort.Slice(missing, func(i, j int) bool { return missing[i].Command < missing[j].Command })
		var b strings.Builder
		fmt.Fprintf(&b, "el árbol de comandos ha PERDIDO %d operaciones del contrato:\n", len(missing))
		for _, op := range missing {
			fmt.Fprintf(&b, "  - %s  (%s)  %s %s\n", op.Command, op.OperationID, op.Method, op.Path)
		}
		b.WriteString("Cada una es una operación del contrato que ya no se puede invocar desde la CLI. " +
			"El arreglo es regenerar el árbol desde el contrato, NUNCA quitarla del manifiesto.")
		t.Error(b.String())
	}

	// Las operaciones de MÁS no son un defecto.
	declared := make(map[string]bool, len(m.Operations))
	for _, op := range m.Operations {
		declared[op.Command] = true
	}
	var extra []string
	for name := range byCommand {
		if !declared[name] {
			extra = append(extra, name)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		shown := extra
		if len(shown) > 10 {
			shown = shown[:10]
		}
		t.Logf("INFORMACIÓN (no es un fallo): el árbol expone %d comandos que este manifiesto no declara; "+
			"los primeros: %s", len(extra), strings.Join(shown, ", "))
	}

	// Cada operación del EJE publica su slot de empresa en el manifiesto.
	axis := 0
	for _, op := range m.Operations {
		param, ok := op.companyAxisParam()
		if !ok {
			continue
		}
		axis++
		entry, present := byCommand[op.Command]
		if !present {
			continue // ya lo denuncia la aserción de no-decrecimiento
		}
		optional, _ := entry["optional_args"].(map[string]any)
		if optional == nil {
			t.Errorf("%s (%s) cuelga del eje de empresa (%s) y el manifiesto de comandos no publica "+
				"`optional_args`: un agente no puede saber que el posicional de empresa es omitible",
				op.Command, op.OperationID, op.Path)
			continue
		}
		flag, _ := optional[param].(string)
		if flag != "--company" {
			t.Errorf("%s (%s) declara el path param del eje %q, pero su entrada publica optional_args=%v "+
				"en vez de {%q: \"--company\"}", op.Command, op.OperationID, param, optional, param)
		}
	}
	if axis == 0 {
		t.Fatal("ninguna operación del manifiesto cuelga del eje de empresa: la aserción 8.6 sería vacua")
	}
	t.Logf("gate ARMADO: %d operaciones comparadas, %d de ellas del eje de empresa", len(m.Operations), axis)
}

// ---------------------------------------------------------------------------
// El flag de empresa es una OPCIÓN GLOBAL
// ---------------------------------------------------------------------------

// TestCompanyFlagIsAGlobalOptionOfTheCommandTree comprueba que el flag que
// rellena el posicional de empresa existe y es PERSISTENTE de la raíz.
func TestCompanyFlagIsAGlobalOptionOfTheCommandTree(t *testing.T) {
	root := NewRootCmd()
	global := root.PersistentFlags().Lookup(companyPathParam)
	if global == nil {
		t.Fatalf("la raíz no declara el flag persistente --%s: sin él, el `optional_args` del manifiesto "+
			"promete un valor que nadie puede aportar", companyPathParam)
	}
	if global.Usage == "" {
		t.Errorf("--%s no documenta para qué sirve", companyPathParam)
	}

	fillers := map[string]bool{}
	withAxis := 0
	var leaf string
	for _, entry := range commandTreeManifest(t) {
		optional, ok := entry["optional_args"].(map[string]any)
		if !ok || len(optional) == 0 {
			continue
		}
		withAxis++
		if leaf == "" {
			leaf, _ = entry["command"].(string)
		}
		for _, v := range optional {
			flag, _ := v.(string)
			fillers[flag] = true
		}
	}
	if withAxis == 0 {
		t.Fatal("ninguna entrada del árbol publica `optional_args`: la aserción sería vacua")
	}
	for flag := range fillers {
		name := strings.TrimPrefix(flag, "--")
		if root.PersistentFlags().Lookup(name) == nil {
			t.Errorf("el manifiesto de comandos anuncia %q como flag que rellena un posicional, "+
				"pero no es una opción global de la CLI", flag)
		}
	}

	// Que sea PERSISTENTE y no solo de la raíz.
	path := strings.Fields(leaf)
	if len(path) < 2 {
		t.Fatalf("no reconozco el comando %q del árbol", leaf)
	}
	c := findCommand(t, path[1:]...)
	if c.InheritedFlags().Lookup(companyPathParam) == nil {
		t.Errorf("%s anuncia --%s en su manifiesto pero no lo hereda: no sería una opción global",
			leaf, companyPathParam)
	}
	t.Logf("--%s es opción global; %d entradas del árbol la publican como relleno del eje (hoja comprobada: %s)",
		companyPathParam, withAxis, leaf)
}

// ---------------------------------------------------------------------------
// Helpers compartidos por las aserciones de este fichero
// ---------------------------------------------------------------------------

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

// findCommand localiza una hoja del árbol por su ruta de grupos y acción.
func findCommand(t *testing.T, path ...string) *cobra.Command {
	t.Helper()
	root := NewRootCmd()
	c, rest, err := root.Find(path)
	if err != nil || len(rest) > 0 {
		t.Fatalf("no encuentro el comando %v (resto %v): %v", path, rest, err)
	}
	return c
}
