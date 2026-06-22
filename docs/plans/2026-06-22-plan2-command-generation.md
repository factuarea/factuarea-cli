# Factuarea CLI — Plan 2: Generación de comandos desde el OpenAPI

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cobertura completa de los ~262 endpoints v1 como árbol de comandos `factuarea <recurso> [<sub>] <accion>` **generado desde el `openapi.json`** (cero mantenimiento manual, cero drift), reusando el runtime del Plan 1 (client/output/safety/config), con soporte de respuestas binarias, uploads multipart, paginación cursor e idempotencia.

**Architecture:** El generador corre en **build-time** (`make generate`): parsea el spec embebido con `pb33f/libopenapi`, pivota en el `operationId` (`public-api.v1.{recurso}[.{sub}].{accion}`) replicando el algoritmo `resolveNamespace`/`toCamelCase` del backend, y emite una **tabla de datos compacta** `resources_gen.go` (`[]genOp`). En **runtime**, un builder a mano convierte esa tabla en el árbol Cobra (arranque instantáneo — NO se parsea el spec de 3MB en cada invocación). Los comandos a mano **ganan** sobre los generados por **exclusión keyed por `operationId`**.

**Tech Stack:** Go 1.26, `github.com/pb33f/libopenapi` (parseo/validación OpenAPI 3.1), Cobra, más todo `internal/*` del Plan 1.

## Global Constraints

- Módulo `github.com/factuarea/factuarea-cli`. Binario `factuarea`.
- **Eje de generación = `operationId`**, formato `public-api.v1.{recurso}[.{sub}].{accion}`. **Invariante de build:** todo `operationId` empieza por `public-api.v1.` y tiene ≥2 segmentos tras el prefijo; si no, **el build FALLA** (no silencioso).
- **Árbol = espejo del contrato** (decisión cerrada): grupos = todos los segmentos menos el último; acción = último segmento; todos en **kebab-case** (`_`→`-`). Ej.: `invoices.payments_create` → `factuarea invoices payments-create`; `verifactu.records.list` → `factuarea verifactu records list`. El último segmento es **único por recurso** (verificado en el spec real) → sin colisiones.
- **Body por `-d <json>` / `--data-file <path>`** (JSON crudo). El `--help` muestra el `example` real que trae el spec por operación. NO se generan flags tipados por campo.
- **Multipart uploads** (4 ops): flag `--file <campo>=<path>`; el runtime arma multipart real (no base64). Campos binarios detectados por `format:binary` en el schema del requestBody `multipart/form-data`.
- **Respuestas binarias** (~9 ops, `application/pdf`/`application/zip`/`application/xml`/`application/octet-stream`): flag `-o/--output <file>`; a stdout solo si no es TTY; nunca parsear como JSON.
- **Paginación cursor**: detectar por presencia del query param `starting_after` (NO por `x-speakeasy-pagination`, que es no-op en casi todo). `--paginate` itera con `has_more`/`next_cursor`.
- **Guards heredados**: comandos de métodos mutadores (POST/PUT/PATCH/DELETE) aplican `safety.RequireLive` ANTES de la red; los `Args` se envuelven con `cmd.UsageArgs` (exit 2). Confirmación tipada para irreversibles → **fuera de Plan 2** (no hay metadata de irreversibilidad en el spec; se aborda con el mismo backend change del scope, ver Out-of-scope).
- **Override determinista**: el generado se registra solo si NO existe un override a mano para ese `operationId`. Drift-guard de CI.
- `$ref`/`allOf` masivos (1021): el parseo se hace con `libopenapi` (resuelve refs); el body de la petición se pasa CRUDO (no se resuelve el schema del body en flags), pero SÍ se resuelve el schema del requestBody multipart (para los nombres de campo binario) y el content de la respuesta 200 (para detectar binario).
- Mensajes en español. TDD. Reusa el runtime del Plan 1; NO dupliques auth/retries/idempotencia/errores/exit-codes.

**Out-of-scope de Plan 2 (trackeado, NO implementar aquí):**
- **Scope-check**: el scope requerido NO está en el spec (solo en el middleware del backend, y no derivable del `operationId`). Se aborda con un **change de backend (OpenSpec)** que publique `x-required-scope` por operación vía `VendorExtensionsTransformer`; cuando exista, una fase posterior añade el aviso de scope local. Mientras tanto el 403 del backend (exit 4) protege.
- **Confirmación tipada por operación irreversible**: requiere metadata `x-irreversible` (mismo backend change). El guard `--live` SÍ aplica a mutadores en Plan 2; la confirmación tipada de irreversibles se cablea cuando el spec marque cuáles.

---

### Task 1: Vendorizar y embeber el spec + SpecHash + `make generate`

**Files:**
- Create: `spec/openapi.json` (spec de develop, pineado)
- Create: `internal/spec/embed.go`
- Modify: `Makefile`
- Modify: `internal/buildinfo/buildinfo.go` (doc del SpecHash)

**Interfaces:**
- Produces: `spec.Raw []byte` (spec embebido), `spec.Hash() string` (sha256 hex del spec embebido).

- [ ] **Step 1: Obtener el spec de develop**

Primario (spec publicado): 
```bash
cd /Users/chelu/Personal/factuarea-cli
curl -fsSL https://api.factuarea.com/v1/openapi.json -o spec/openapi.json
```
Alternativa local (backend en Docker, rama develop):
```bash
docker exec factuarea-backend php artisan scramble:export --api=public-api --path=/tmp/openapi.json
docker cp factuarea-backend:/tmp/openapi.json /Users/chelu/Personal/factuarea-cli/spec/openapi.json
```
Verifica que es 3.1 y tiene los recursos esperados:
```bash
jq -r '.openapi, (.paths|length), ([.paths[]|keys[]]|length)' spec/openapi.json
# espera: 3.1.0, ~214, ~262
jq -r '[.paths[][]?|.operationId]|map(select(startswith("public-api.v1.")|not))|length' spec/openapi.json
# espera: 0 (todos siguen el prefijo)
```

- [ ] **Step 2: Embeber el spec y exponer el hash**

Create `internal/spec/embed.go`:
```go
package spec

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

//go:embed openapi.json
var Raw []byte

// Hash devuelve el sha256 hex del spec embebido (se expone en `factuarea version`).
func Hash() string {
	sum := sha256.Sum256(Raw)
	return hex.EncodeToString(sum[:])
}
```
> Nota: `go:embed` requiere que el fichero esté dentro o por debajo del paquete. Coloca el spec en `internal/spec/openapi.json` (mueve `spec/openapi.json` ahí) y ajusta el `//go:embed openapi.json`. Mantén un symlink o copia en `spec/openapi.json` solo si lo prefieres para `make generate`; lo canónico embebido vive en `internal/spec/`.

- [ ] **Step 3: Wirear SpecHash en `version`**

`buildinfo.SpecHash` ya existe (Plan 1) como var inyectable. En vez de ldflags, derívalo del spec embebido en tiempo de ejecución para que SIEMPRE refleje el spec real. Modifica `internal/cmd/version.go` para imprimir `spec.Hash()[:12]` en lugar de `buildinfo.SpecHash` (importa `internal/spec`). Deja `buildinfo.SpecHash` como estaba (no se usa ya; anótalo). 

Update `internal/cmd/version.go` línea del Fprintf:
```go
fmt.Fprintf(cmd.OutOrStdout(), "factuarea %s (commit %s, spec %s)\n",
	buildinfo.Version, buildinfo.Commit, spec.Hash()[:12])
```
y ajusta el test `version_test.go` si comprueba el texto del spec (sigue conteniendo "factuarea").

- [ ] **Step 4: `make generate`**

Add to `Makefile`:
```makefile
SPEC_URL ?= https://api.factuarea.com/v1/openapi.json
generate:
	curl -fsSL $(SPEC_URL) -o internal/spec/openapi.json
	go generate ./...
```
(`go generate` se cablea en Task 4 con la directiva del generador.)

- [ ] **Step 5: Verificar y commit**

Run: `cd /Users/chelu/Personal/factuarea-cli && go build ./... && go test ./... && go vet ./...`
Expected: verde; `go run ./cmd/factuarea version` imprime `spec <12hex>`.
```bash
git add internal/spec Makefile internal/cmd/version.go internal/buildinfo
git commit -m "feat(spec): vendor and embed develop OpenAPI spec with hash"
```

---

### Task 2: Algoritmo de namespacing operationId → árbol (kebab)

**Files:**
- Create: `internal/spec/namespace.go`
- Test: `internal/spec/namespace_test.go`

**Interfaces:**
- Produces: `spec.Resolve(operationID string) (groups []string, action string, ok bool)` — `groups` y `action` en kebab-case; `ok=false` si no cumple el invariante (`public-api.v1.` + ≥2 segmentos). `spec.ToKebab(string) string`.

- [ ] **Step 1: Test del algoritmo (con los gotchas reales)**

Create `internal/spec/namespace_test.go`:
```go
package spec

import (
	"reflect"
	"testing"
)

func TestResolve(t *testing.T) {
	cases := []struct {
		id     string
		groups []string
		action string
		ok     bool
	}{
		{"public-api.v1.invoices.create", []string{"invoices"}, "create", true},
		{"public-api.v1.invoices.mark_paid", []string{"invoices"}, "mark-paid", true},
		{"public-api.v1.invoices.payments_create", []string{"invoices"}, "payments-create", true}, // underscore plano: NO promueve sub-nivel
		{"public-api.v1.verifactu.records.find_by_csv", []string{"verifactu", "records"}, "find-by-csv", true}, // sub-nivel real
		{"public-api.v1.products.gallery.upload", []string{"products", "gallery"}, "upload", true},
		{"public-api.v1.delivery_notes.list", []string{"delivery-notes"}, "list", true},
		{"public-api.v1.stripe_autoinvoicing.accounts.list", []string{"stripe-autoinvoicing", "accounts"}, "list", true},
		{"public-api.v1.webhook_endpoints.deliveries.replay", []string{"webhook-endpoints", "deliveries"}, "replay", true},
		{"public-api.v1.account", nil, "", false},     // <2 segmentos
		{"some.other.id", nil, "", false},             // sin prefijo
	}
	for _, c := range cases {
		g, a, ok := Resolve(c.id)
		if ok != c.ok || a != c.action || !reflect.DeepEqual(g, c.groups) {
			t.Errorf("Resolve(%q) = (%v,%q,%v), want (%v,%q,%v)", c.id, g, a, ok, c.groups, c.action, c.ok)
		}
	}
}
```

- [ ] **Step 2: Run → FAIL** (`go test ./internal/spec/ -run TestResolve`).

- [ ] **Step 3: Implementar**

Create `internal/spec/namespace.go`:
```go
package spec

import "strings"

const operationIDPrefix = "public-api.v1."

// Resolve trocea el operationId en (grupos, acción) espejando resolveNamespace
// del backend: quita el prefijo, separa por '.', el último segmento es la acción
// y el resto son los grupos. Todos en kebab-case. ok=false si no cumple el
// invariante (prefijo + >=2 segmentos).
func Resolve(operationID string) (groups []string, action string, ok bool) {
	if !strings.HasPrefix(operationID, operationIDPrefix) {
		return nil, "", false
	}
	rest := strings.TrimPrefix(operationID, operationIDPrefix)
	segs := strings.Split(rest, ".")
	if len(segs) < 2 {
		return nil, "", false
	}
	action = ToKebab(segs[len(segs)-1])
	for _, s := range segs[:len(segs)-1] {
		groups = append(groups, ToKebab(s))
	}
	return groups, action, true
}

// ToKebab pasa un segmento de operationId a kebab-case para el CLI.
func ToKebab(s string) string { return strings.ReplaceAll(s, "_", "-") }
```

- [ ] **Step 4: Run → PASS. Commit.**
```bash
git add internal/spec/namespace.go internal/spec/namespace_test.go
git commit -m "feat(spec): operationId -> kebab command-tree resolver"
```

---

### Task 3: Modelo y parseo del spec con libopenapi

**Files:**
- Create: `internal/spec/model.go`
- Create: `internal/spec/load.go`
- Test: `internal/spec/load_test.go`

**Interfaces:**
- Consumes: `spec.Raw` (Task 1), `spec.Resolve` (Task 2).
- Produces:
  - `spec.Operation{OperationID, Method, Path, Groups []string, Action, Summary, Deprecated bool, PathParams []Param, QueryParams []Param, Body *Body, BinaryResponse *BinaryResponse}` + métodos `Mutating() bool`, `Paginated() bool`.
  - `spec.Param{Name, In, Required bool, Type, Description}`.
  - `spec.Body{Kind string /* "json"|"multipart" */, Example string, FileFields []string}`.
  - `spec.BinaryResponse{ContentType string}`.
  - `spec.Load() ([]Operation, error)` — parsea el spec embebido, valida el invariante de operationId (error si falla), devuelve las operaciones ordenadas determinísticamente (por path+method).

- [ ] **Step 1: Añadir libopenapi**

```bash
cd /Users/chelu/Personal/factuarea-cli && go get github.com/pb33f/libopenapi@latest
```

- [ ] **Step 2: Test contra el spec REAL embebido**

Create `internal/spec/load_test.go`:
```go
package spec

import "testing"

func TestLoadParsesRealSpec(t *testing.T) {
	ops, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(ops) < 200 {
		t.Fatalf("expected >=200 operations, got %d", len(ops))
	}
	by := map[string]Operation{}
	for _, o := range ops {
		by[o.OperationID] = o
	}

	// CRUD con path param + body json + ejemplo
	create := by["public-api.v1.invoices.create"]
	if create.Method != "POST" || create.Body == nil || create.Body.Kind != "json" || create.Body.Example == "" {
		t.Errorf("invoices.create mal parseado: %+v", create)
	}
	if !create.Mutating() {
		t.Error("invoices.create debe ser mutating")
	}

	// list paginado
	list := by["public-api.v1.invoices.list"]
	if !list.Paginated() {
		t.Error("invoices.list debe detectarse como paginado (starting_after)")
	}

	// path param
	get := by["public-api.v1.invoices.get"]
	if len(get.PathParams) != 1 || get.PathParams[0].Name == "" {
		t.Errorf("invoices.get debe tener 1 path param: %+v", get.PathParams)
	}

	// respuesta binaria
	pdf := by["public-api.v1.invoices.pdf"]
	if pdf.BinaryResponse == nil || pdf.BinaryResponse.ContentType != "application/pdf" {
		t.Errorf("invoices.pdf debe tener BinaryResponse pdf: %+v", pdf.BinaryResponse)
	}

	// multipart upload con campo binario
	up := by["public-api.v1.verifactu.certificates.upload"]
	if up.Body == nil || up.Body.Kind != "multipart" || len(up.Body.FileFields) == 0 {
		t.Errorf("certificates.upload debe ser multipart con FileFields: %+v", up.Body)
	}
}
```

- [ ] **Step 3: Run → FAIL.**

- [ ] **Step 4: Implementar model.go y load.go**

Create `internal/spec/model.go`:
```go
package spec

type Operation struct {
	OperationID    string
	Method         string // GET, POST, PUT, PATCH, DELETE
	Path           string // /invoices/{invoice}
	Groups         []string
	Action         string
	Summary        string
	Deprecated     bool
	PathParams     []Param
	QueryParams    []Param
	Body           *Body
	BinaryResponse *BinaryResponse
}

type Param struct {
	Name        string
	In          string // path | query
	Required    bool
	Type        string
	Description  string
}

type Body struct {
	Kind       string // "json" | "multipart"
	Example    string // JSON example (solo json)
	FileFields []string // campos binarios (solo multipart)
}

type BinaryResponse struct{ ContentType string }

func (o Operation) Mutating() bool {
	switch o.Method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

func (o Operation) Paginated() bool {
	for _, p := range o.QueryParams {
		if p.Name == "starting_after" {
			return true
		}
	}
	return false
}
```

Create `internal/spec/load.go`:
```go
package spec

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// Load parsea el spec embebido (OpenAPI 3.1) y devuelve las operaciones del CLI.
// Valida el invariante de operationId: cada una debe resolver vía Resolve(); si
// alguna no, devuelve error (build/arranque falla, no silencioso).
func Load() ([]Operation, error) {
	doc, err := libopenapi.NewDocument(Raw)
	if err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	model, errs := doc.BuildV3Model()
	if len(errs) > 0 {
		return nil, fmt.Errorf("build v3 model: %v", errs)
	}
	var ops []Operation
	for pair := model.Model.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
		path := pair.Key()
		item := pair.Value()
		for method, op := range methodsOf(item) {
			if op == nil || op.OperationId == "" {
				continue
			}
			groups, action, ok := Resolve(op.OperationId)
			if !ok {
				return nil, fmt.Errorf("operationId no cumple el invariante public-api.v1.{recurso}.{accion}: %q (%s %s)", op.OperationId, method, path)
			}
			ops = append(ops, buildOperation(op, method, path, groups, action))
		}
	}
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Path != ops[j].Path {
			return ops[i].Path < ops[j].Path
		}
		return ops[i].Method < ops[j].Method
	})
	return ops, nil
}

func methodsOf(item *v3.PathItem) map[string]*v3.Operation {
	m := map[string]*v3.Operation{}
	if item.Get != nil {
		m["GET"] = item.Get
	}
	if item.Post != nil {
		m["POST"] = item.Post
	}
	if item.Put != nil {
		m["PUT"] = item.Put
	}
	if item.Patch != nil {
		m["PATCH"] = item.Patch
	}
	if item.Delete != nil {
		m["DELETE"] = item.Delete
	}
	return m
}

func buildOperation(op *v3.Operation, method, path string, groups []string, action string) Operation {
	o := Operation{
		OperationID: op.OperationId,
		Method:      method,
		Path:        path,
		Groups:      groups,
		Action:      action,
		Summary:     op.Summary,
		Deprecated:  op.Deprecated != nil && *op.Deprecated,
	}
	if op.Parameters != nil {
		for _, p := range op.Parameters {
			param := Param{Name: p.Name, In: p.In, Description: p.Description}
			if p.Required != nil {
				param.Required = *p.Required
			}
			if p.Schema != nil {
				if sc := p.Schema.Schema(); sc != nil && len(sc.Type) > 0 {
					param.Type = sc.Type[0]
				}
			}
			switch p.In {
			case "path":
				o.PathParams = append(o.PathParams, param)
			case "query":
				o.QueryParams = append(o.QueryParams, param)
			}
		}
	}
	o.Body = buildBody(op)
	o.BinaryResponse = buildBinaryResponse(op)
	return o
}

// buildBody detecta json (con example) o multipart (con campos binarios).
func buildBody(op *v3.Operation) *Body {
	if op.RequestBody == nil || op.RequestBody.Content == nil {
		return nil
	}
	if mt, ok := op.RequestBody.Content.Get("application/json"); ok {
		b := &Body{Kind: "json"}
		if mt.Example != nil {
			if raw, err := json.Marshal(mt.Example); err == nil {
				b.Example = string(raw)
			}
		}
		return b
	}
	if mt, ok := op.RequestBody.Content.Get("multipart/form-data"); ok && mt.Schema != nil {
		b := &Body{Kind: "multipart"}
		if sc := mt.Schema.Schema(); sc != nil && sc.Properties != nil {
			for prop := sc.Properties.First(); prop != nil; prop = prop.Next() {
				if ps := prop.Value().Schema(); ps != nil && ps.Format == "binary" {
					b.FileFields = append(b.FileFields, prop.Key())
				}
			}
		}
		return b
	}
	return nil
}

// buildBinaryResponse detecta una respuesta 200 cuyo content NO es JSON.
func buildBinaryResponse(op *v3.Operation) *BinaryResponse {
	if op.Responses == nil || op.Responses.Codes == nil {
		return nil
	}
	resp, ok := op.Responses.Codes.Get("200")
	if !ok || resp.Content == nil {
		return nil
	}
	for ct := resp.Content.First(); ct != nil; ct = ct.Next() {
		mediaType := ct.Key()
		if strings.Contains(mediaType, "json") {
			return nil // JSON normal
		}
		// primer content no-JSON con schema binary → respuesta binaria
		if sc := ct.Value().Schema; sc != nil {
			if s := sc.Schema(); s != nil && s.Format == "binary" {
				return &BinaryResponse{ContentType: mediaType}
			}
		}
	}
	return nil
}
```
> Nota de implementación: las firmas exactas de `libopenapi` (orderedmap `.First()/.Next()`, `Schema()` para resolver `$ref`) pueden variar ligeramente por versión; el implementador debe ajustar a la API real de la versión instalada (compilar contra ella) manteniendo la SEMÁNTICA descrita. Validar contra el spec real con el test.

- [ ] **Step 5: Run → PASS (contra el spec real). Commit.**
```bash
git add internal/spec/model.go internal/spec/load.go internal/spec/load_test.go go.mod go.sum
git commit -m "feat(spec): parse OpenAPI 3.1 into CLI operation model (libopenapi)"
```

---

### Task 4: Generador build-time → `resources_gen.go` (tabla compacta)

**Files:**
- Create: `internal/gen/main.go`
- Create: `internal/gen/gen.go`
- Create: `gen.go` (raíz, con la directiva `//go:generate`)
- Create: `internal/cmd/resources_gen.go` (GENERADO)
- Test: `internal/gen/gen_test.go`

**Interfaces:**
- Produces: `internal/cmd/resources_gen.go` con `func generatedOps() []genOp` y el tipo `genOp` (espejo serializable de `spec.Operation`: campos exportados con los datos necesarios para construir el comando en runtime). El runtime (Task 6) consume `generatedOps()`.

**Design:** el generador NO emite constructores de Cobra (frágil); emite una **tabla de datos** `[]genOp`. El builder a mano (Task 6) la convierte en comandos. Arranque instantáneo: no se parsea libopenapi en runtime.

- [ ] **Step 1: Definir `genOp` y el generador**

Create `internal/gen/gen.go` (lógica de emisión, con `text/template`):
```go
package gen

import (
	"bytes"
	"fmt"
	"go/format"
	"text/template"

	"github.com/factuarea/factuarea-cli/internal/spec"
)

// Generate parsea el spec embebido y devuelve el contenido de resources_gen.go.
func Generate() ([]byte, error) {
	ops, err := spec.Load()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ops); err != nil {
		return nil, err
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("gofmt del código generado: %w\n%s", err, buf.String())
	}
	return formatted, nil
}

var tmpl = template.Must(template.New("gen").Funcs(template.FuncMap{
	"q": func(s string) string { return fmt.Sprintf("%q", s) },
}).Parse(`// Code generated by internal/gen; DO NOT EDIT.
package cmd

type genParam struct {
	Name, In, Type, Description string
	Required                    bool
}
type genBody struct {
	Kind, Example string
	FileFields    []string
}
type genOp struct {
	OperationID, Method, Path, Action, Summary string
	Deprecated                                 bool
	Groups                                     []string
	PathParams, QueryParams                    []genParam
	Body                                       *genBody
	BinaryContentType                          string
}

func generatedOps() []genOp {
	return []genOp{
{{- range .}}
		{
			OperationID: {{q .OperationID}}, Method: {{q .Method}}, Path: {{q .Path}},
			Action: {{q .Action}}, Summary: {{q .Summary}}, Deprecated: {{.Deprecated}},
			Groups: []string{ {{range .Groups}}{{q .}}, {{end}} },
			PathParams: []genParam{ {{range .PathParams}}{Name: {{q .Name}}, In: {{q .In}}, Type: {{q .Type}}, Required: {{.Required}}}, {{end}} },
			QueryParams: []genParam{ {{range .QueryParams}}{Name: {{q .Name}}, In: {{q .In}}, Type: {{q .Type}}, Required: {{.Required}}}, {{end}} },
			{{if .Body}}Body: &genBody{Kind: {{q .Body.Kind}}, Example: {{q .Body.Example}}, FileFields: []string{ {{range .Body.FileFields}}{{q .}}, {{end}} }},{{end}}
			{{if .BinaryResponse}}BinaryContentType: {{q .BinaryResponse.ContentType}},{{end}}
		},
{{- end}}
	}
}
`))
```

Create `internal/gen/main.go` (binario del generador):
```go
//go:build ignore

package main

import (
	"os"

	"github.com/factuarea/factuarea-cli/internal/gen"
)

func main() {
	out, err := gen.Generate()
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile("internal/cmd/resources_gen.go", out, 0o644); err != nil {
		panic(err)
	}
}
```
> Nota: `main.go` lleva `//go:build ignore` para no compilarse en el binario; se ejecuta con `go run internal/gen/main.go`. Como importa `internal/gen` (que importa `internal/spec`, que embebe el spec), la generación parsea el spec embebido del propio repo.

Create `gen.go` (raíz):
```go
package cli

//go:generate go run internal/gen/main.go
```
(El package name de la raíz puede ser cualquiera no-main; usa `cli` o muévelo a `internal/`. Ajusta para que `go generate ./...` lo encuentre.)

- [ ] **Step 2: Generar y verificar que compila**

Run:
```bash
cd /Users/chelu/Personal/factuarea-cli && go run internal/gen/main.go && go build ./...
```
Expected: crea `internal/cmd/resources_gen.go`, compila.

- [ ] **Step 3: Test del generador**

Create `internal/gen/gen_test.go`:
```go
package gen

import (
	"strings"
	"testing"
)

func TestGenerateProducesCompilableTable(t *testing.T) {
	out, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "func generatedOps() []genOp") {
		t.Fatal("falta generatedOps()")
	}
	if !strings.Contains(s, `OperationID: "public-api.v1.invoices.create"`) {
		t.Fatal("falta invoices.create en la tabla generada")
	}
	if strings.Count(s, "OperationID:") < 200 {
		t.Fatalf("esperaba >=200 ops, got %d", strings.Count(s, "OperationID:"))
	}
}
```

- [ ] **Step 4: Run → PASS. Commit (incluye el resources_gen.go generado).**
```bash
git add internal/gen gen.go internal/cmd/resources_gen.go
git commit -m "feat(gen): build-time generator emits compact command table from spec"
```

---

### Task 5: Soporte de runtime — Paginate, idempotency-key, multipart, binario

**Files:**
- Create: `internal/client/paginate.go`
- Modify: `internal/client/client.go` (multipart helper + idempotency option)
- Test: `internal/client/paginate_test.go`, `internal/client/multipart_test.go`

**Interfaces:**
- Produces:
  - `(*Client).Paginate(ctx, path string, query url.Values, each func(item json.RawMessage) error) error` — itera `starting_after` usando `has_more`/`next_cursor`, llamando `each` por cada objeto de `data`.
  - `client.MultipartBody(fields map[string]string, files map[string]string) (body []byte, contentType string, err error)` — arma un multipart/form-data real.
  - `(*Client).Do` ya acepta `extraHeaders["Idempotency-Key"]` y `extraHeaders["Content-Type"]` (para multipart). Verifica que un Content-Type explícito en extraHeaders NO se pisa por el `application/json` por defecto.

- [ ] **Step 1: Tests (httptest) de Paginate y multipart**

Create `internal/client/paginate_test.go`:
```go
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

func TestPaginateFollowsCursor(t *testing.T) {
	page := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("starting_after") == "" {
			_, _ = fmt.Fprint(w, `{"data":[{"id":"a"},{"id":"b"}],"has_more":true,"next_cursor":"b"}`)
		} else {
			_, _ = fmt.Fprint(w, `{"data":[{"id":"c"}],"has_more":false,"next_cursor":null}`)
		}
		page++
	})
	var ids []string
	err := c.Paginate(context.Background(), "/v1/invoices", url.Values{}, func(item json.RawMessage) error {
		var o struct{ ID string `json:"id"` }
		_ = json.Unmarshal(item, &o)
		ids = append(ids, o.ID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(ids) != "[a b c]" {
		t.Fatalf("got %v", ids)
	}
}
```

Create `internal/client/multipart_test.go`:
```go
package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultipartBody(t *testing.T) {
	f := filepath.Join(t.TempDir(), "cert.p12")
	_ = os.WriteFile(f, []byte("BINARY"), 0o600)
	body, ct, err := MultipartBody(map[string]string{"certificate_password": "x"}, map[string]string{"certificate_file": f})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ct, "multipart/form-data; boundary=") {
		t.Fatalf("content-type: %q", ct)
	}
	if !strings.Contains(string(body), "certificate_password") || !strings.Contains(string(body), "BINARY") {
		t.Fatal("multipart no contiene campo/archivo")
	}
}
```

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implementar Paginate y MultipartBody**

Create `internal/client/paginate.go`:
```go
package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

type pageEnvelope struct {
	Data       []json.RawMessage `json:"data"`
	HasMore    bool              `json:"has_more"`
	NextCursor *string           `json:"next_cursor"`
}

// Paginate itera todas las páginas de un listado cursor, invocando each por
// objeto. Degrada a una sola página si la respuesta no trae has_more/next_cursor.
func (c *Client) Paginate(ctx context.Context, path string, query url.Values, each func(json.RawMessage) error) error {
	for {
		u := path
		if len(query) > 0 {
			u = path + "?" + query.Encode()
		}
		resp, err := c.Do(ctx, http.MethodGet, u, nil, nil)
		if err != nil {
			return err
		}
		var env pageEnvelope
		if err := json.Unmarshal(resp.Body, &env); err != nil {
			return err
		}
		for _, item := range env.Data {
			if err := each(item); err != nil {
				return err
			}
		}
		if !env.HasMore || env.NextCursor == nil || *env.NextCursor == "" {
			return nil
		}
		query.Set("starting_after", *env.NextCursor)
	}
}
```

Add to `internal/client/client.go` (multipart helper):
```go
// MultipartBody arma un cuerpo multipart/form-data con campos de texto y ficheros.
func MultipartBody(fields, files map[string]string) ([]byte, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}
	for field, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return nil, "", err
		}
		w, err := mw.CreateFormFile(field, filepath.Base(path))
		if err != nil {
			f.Close()
			return nil, "", err
		}
		if _, err := io.Copy(w, f); err != nil {
			f.Close()
			return nil, "", err
		}
		f.Close()
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}
```
(añade imports `mime/multipart`, `os`, `path/filepath`, `io` a client.go.)

Verifica que en `Do`, si `extraHeaders["Content-Type"]` viene seteado (multipart), el `req.Header.Set("Content-Type","application/json")` del body NO lo pisa: el loop de `extraHeaders` corre DESPUÉS, así que ya gana. Confírmalo con un comentario y, si no, reordena para que extraHeaders tenga prioridad.

- [ ] **Step 4: Run → PASS. Commit.**
```bash
git add internal/client/paginate.go internal/client/client.go internal/client/paginate_test.go internal/client/multipart_test.go
git commit -m "feat(client): cursor pagination, multipart bodies, idempotency-key passthrough"
```

---

### Task 6: Builder del árbol Cobra + override determinista + drift-guard

**Files:**
- Create: `internal/cmd/build_generated.go`
- Create: `internal/cmd/register.go`
- Modify: `internal/cmd/root.go` (registrar el árbol generado)
- Create: `internal/cmd/override/` (vacío por ahora; punto de extensión)
- Test: `internal/cmd/build_generated_test.go`

**Interfaces:**
- Consumes: `generatedOps()` (Task 4), `client`, `output`, `safety`, `cmd.UsageArgs`.
- Produces: `buildResourceTree(reg *resourceRegistrar)` que crea y cuelga del root los comandos generados; `overrides` (map `operationID → *cobra.Command`) — vacío en Plan 2, lo rellenarán comandos a mano futuros.

- [ ] **Step 1: Test del árbol generado (httptest end-to-end)**

Create `internal/cmd/build_generated_test.go`:
```go
package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	return out.String, root.Execute()
}

func TestGeneratedListCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/invoices" {
			t.Errorf("path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"inv_1"}],"has_more":false,"next_cursor":null}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"invoices", "list", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), `"id":"inv_1"`) {
		t.Fatalf("salida: %s", out.String())
	}
}

func TestGeneratedMutatingInheritsLiveGuard(t *testing.T) {
	t.Setenv("FACTUAREA_API_KEY", "fact_live_bbbbbbbbbbbbbbbbbbbbbbbb")
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	// invoices create (POST) en live sin --live debe fallar ANTES de red.
	root.SetArgs([]string{"invoices", "create", "-d", "{}"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "LIVE") {
		t.Fatalf("esperaba guard LIVE, got %v", err)
	}
}
```

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implementar el builder y el registrador**

Create `internal/cmd/build_generated.go`:
```go
package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
)

// buildGeneratedCommand convierte un genOp en un *cobra.Command que reusa el
// runtime del Plan 1. Args posicionales = path params; flags = query params;
// -d/--data + --data-file para el body json; --file para multipart; -o para
// respuestas binarias; --paginate para listados cursor.
func buildGeneratedCommand(op genOp) *cobra.Command {
	var data, dataFile, output_ string
	var paginate bool
	fileFlags := map[string]*string{}

	use := op.Action
	for _, p := range op.PathParams {
		use += " <" + p.Name + ">"
	}
	long := op.Summary
	if op.Body != nil && op.Body.Kind == "json" && op.Body.Example != "" {
		long += "\n\nEjemplo de body (--data):\n  " + op.Body.Example
	}

	c := &cobra.Command{
		Use:        use,
		Short:      op.Summary,
		Long:       strings.TrimSpace(long),
		Args:       UsageArgs(cobra.ExactArgs(len(op.PathParams))),
		Deprecated: deprecatedMsg(op),
		RunE: func(cmd *cobra.Command, args []string) error {
			g := globalsFrom(cmd)
			cc, err := newCLIContext(g, "")
			if err != nil {
				return err
			}
			if op.isMutating() {
				if err := safety.RequireLive(cc.res.Environment, g.Live); err != nil {
					return err
				}
			}
			path := op.buildPath(args)
			q := url.Values{}
			for _, p := range op.QueryParams {
				if v, _ := cmd.Flags().GetString(p.Name); v != "" {
					q.Set(p.Name, v)
				}
			}

			// Listado paginado con --paginate.
			if op.isPaginated() && paginate {
				return runPaginated(cmd, cc, path, q)
			}

			body, headers, err := op.buildBody(data, dataFile, fileFlags)
			if err != nil {
				return err
			}
			full := path
			if len(q) > 0 {
				full += "?" + q.Encode()
			}
			resp, err := cc.client.Do(context.Background(), op.Method, full, body, headers)
			if err != nil {
				output.PrintError(cmd.ErrOrStderr(), err, cc.format)
				return &AlreadyReported{Err: err}
			}
			// Respuesta binaria → fichero / stdout no-TTY.
			if op.BinaryContentType != "" {
				return writeBinary(cmd, resp.Body, output_)
			}
			if g.Verbose && resp.RequestID != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "request_id: %s\n", resp.RequestID)
			}
			return output.PrintBody(cmd.OutOrStdout(), resp.Body, cc.format)
		},
	}
	for _, p := range op.QueryParams {
		c.Flags().String(p.Name, "", p.Description)
	}
	if op.Body != nil && op.Body.Kind == "json" {
		c.Flags().StringVarP(&data, "data", "d", "", "cuerpo JSON de la petición")
		c.Flags().StringVar(&dataFile, "data-file", "", "ruta a un fichero con el cuerpo JSON")
	}
	if op.Body != nil && op.Body.Kind == "multipart" {
		for _, ff := range op.Body.FileFields {
			ff := ff
			v := c.Flags().String("file-"+ff, "", "ruta al fichero para el campo "+ff)
			fileFlags[ff] = v
		}
	}
	if op.BinaryContentType != "" {
		c.Flags().StringVarP(&output_, "output", "o", "", "escribe la respuesta binaria a este fichero")
	}
	if op.isPaginated() {
		c.Flags().BoolVar(&paginate, "paginate", false, "recorre todas las páginas (cursor)")
	}
	return c
}

func writeBinary(cmd *cobra.Command, body []byte, out string) error {
	if out != "" {
		return os.WriteFile(out, body, 0o644)
	}
	if output.IsTTY(os.Stdout) {
		return fmt.Errorf("la respuesta es binaria; usa -o <fichero> para guardarla")
	}
	_, err := cmd.OutOrStdout().Write(body)
	return err
}

func deprecatedMsg(op genOp) string {
	if op.Deprecated {
		return "esta operación está deprecada en la API"
	}
	return ""
}
```

Add helper methods on `genOp` (en el mismo paquete, p.ej. `internal/cmd/genop_helpers.go`):
```go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/client"
)

func (op genOp) isMutating() bool {
	switch op.Method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

func (op genOp) isPaginated() bool {
	for _, p := range op.QueryParams {
		if p.Name == "starting_after" {
			return true
		}
	}
	return false
}

func (op genOp) buildPath(args []string) string {
	path := op.Path
	for i, p := range op.PathParams {
		path = strings.Replace(path, "{"+p.Name+"}", args[i], 1)
	}
	if !strings.HasPrefix(path, "/v1") {
		path = "/v1" + path
	}
	return path
}

// buildBody devuelve el cuerpo y headers según json/multipart/sin-body.
func (op genOp) buildBody(data, dataFile string, files map[string]*string) ([]byte, map[string]string, error) {
	if op.Body == nil {
		return nil, nil, nil
	}
	if op.Body.Kind == "json" {
		if dataFile != "" {
			b, err := os.ReadFile(dataFile)
			return b, nil, err
		}
		if data != "" {
			return []byte(data), nil, nil
		}
		return nil, nil, nil // body opcional vacío; la API valida
	}
	// multipart
	fileMap := map[string]string{}
	for field, v := range files {
		if v != nil && *v != "" {
			fileMap[field] = *v
		}
	}
	if len(fileMap) == 0 {
		return nil, nil, fmt.Errorf("falta --file-<campo> para el upload (%s)", strings.Join(op.Body.FileFields, ", "))
	}
	// campos de texto extra del multipart van por --data como JSON plano {campo:valor}
	fields := map[string]string{}
	if data != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(data), &m); err == nil {
			for k, v := range m {
				fields[k] = fmt.Sprint(v)
			}
		}
	}
	body, ct, err := client.MultipartBody(fields, fileMap)
	if err != nil {
		return nil, nil, err
	}
	return body, map[string]string{"Content-Type": ct}, nil
}
```
> Nota: para multipart con campos de texto + fichero (p.ej. `certificates.upload` lleva `certificate_password` + `certificate_file`), el usuario pasa los campos de texto por `--data '{"certificate_password":"x"}'` y el fichero por `--file-certificate_file <path>`. Documenta esto en el `--help` (Long) de los comandos multipart.

Create `internal/cmd/register.go` (override determinista):
```go
package cmd

import "github.com/spf13/cobra"

// overrides: comandos generados que se reemplazan por una versión a mano,
// keyed por operationId. Vacío en Plan 2 (punto de extensión).
var overrides = map[string]func() *cobra.Command{}

// registerGeneratedCommands construye el árbol de recursos y lo cuelga del root.
// Para cada operación generada, si hay override por operationId, usa el override;
// si no, usa el generado. Crea los comandos-grupo intermedios on-demand.
func registerGeneratedCommands(root *cobra.Command) {
	groups := map[string]*cobra.Command{} // clave = ruta de grupo unida por espacio
	groupFor := func(path []string) *cobra.Command {
		parent := root
		key := ""
		for _, seg := range path {
			if key != "" {
				key += " "
			}
			key += seg
			g, ok := groups[key]
			if !ok {
				g = &cobra.Command{Use: seg, Short: "Comandos de " + seg}
				parent.AddCommand(g)
				groups[key] = g
			}
			parent = g
		}
		return parent
	}

	for _, op := range generatedOps() {
		var leaf *cobra.Command
		if mk, ok := overrides[op.OperationID]; ok {
			leaf = mk() // override gana; exclusión en origen
		} else {
			leaf = buildGeneratedCommand(op)
		}
		groupFor(op.Groups).AddCommand(leaf)
	}
}
```

Modify `internal/cmd/root.go`: tras `root.AddCommand(newVersionCmd(), ...)`, añade `registerGeneratedCommands(root)`.

Add `runPaginated` (en build_generated.go) usando `cc.client.Paginate` y emitiendo NDJSON por stdout.

- [ ] **Step 4: Regenerar, run tests → PASS, verificar build/vet/gofmt.**
```bash
go run internal/gen/main.go && go test ./internal/cmd/ -v && go build ./... && go vet ./... && gofmt -l .
```

- [ ] **Step 5: Commit.**
```bash
git add internal/cmd
git commit -m "feat(cmd): build Cobra tree from generated table with deterministic overrides"
```

---

### Task 7: Drift-guard de CI + comando `commands` (manifiesto-máquina)

**Files:**
- Create: `internal/cmd/commands.go`
- Modify: `internal/cmd/root.go` (registrar `commands`)
- Create: `internal/spec/drift_test.go`
- Test: `internal/cmd/commands_test.go`

**Interfaces:**
- Produces: `factuarea commands [--json]` que vuelca el manifiesto por comando `{path, summary, args[], flags[], mutating, deprecated, binary, paginated, example}` desde `generatedOps()`. Drift-guard: test que (si hay red) baja el spec vivo y falla si difiere del embebido.

- [ ] **Step 1: Tests**

Create `internal/cmd/commands_test.go`:
```go
package cmd

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCommandsManifestJSON(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"commands", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var manifest []map[string]any
	if err := json.Unmarshal(out.Bytes(), &manifest); err != nil {
		t.Fatalf("manifest no es JSON: %v", err)
	}
	if len(manifest) < 200 {
		t.Fatalf("esperaba >=200 comandos, got %d", len(manifest))
	}
}
```

Create `internal/spec/drift_test.go`:
```go
package spec

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"testing"
)

// TestSpecNotDrifted baja el spec vivo y falla si el embebido difiere.
// Skippeable offline / en CI sin red con FACTUAREA_SKIP_DRIFT=1.
func TestSpecNotDrifted(t *testing.T) {
	if os.Getenv("FACTUAREA_SKIP_DRIFT") == "1" {
		t.Skip("drift check desactivado")
	}
	resp, err := http.Get("https://api.factuarea.com/v1/openapi.json")
	if err != nil {
		t.Skipf("sin red para drift check: %v", err)
	}
	defer resp.Body.Close()
	live, _ := io.ReadAll(resp.Body)
	sum := sha256.Sum256(live)
	if hex.EncodeToString(sum[:]) != Hash() {
		t.Errorf("el spec embebido difiere del vivo; corre `make generate` y regenera (embedded=%s)", Hash()[:12])
	}
}
```

- [ ] **Step 2: Run → FAIL (commands indefinido).**

- [ ] **Step 3: Implementar `commands.go`**

Create `internal/cmd/commands.go`:
```go
package cmd

import (
	"encoding/json"

	"github.com/spf13/cobra"
)

func newCommandsCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "commands",
		Short: "Vuelca el manifiesto de todos los comandos (discovery para agentes)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			type flagInfo struct {
				Name, Type string
			}
			type manifestEntry struct {
				Command    string     `json:"command"`
				Summary    string     `json:"summary"`
				Args       []string   `json:"args"`
				Flags      []flagInfo `json:"flags"`
				Mutating   bool       `json:"mutating"`
				Deprecated bool       `json:"deprecated"`
				Binary     bool       `json:"binary"`
				Paginated  bool       `json:"paginated"`
				Example    string     `json:"example,omitempty"`
			}
			var manifest []manifestEntry
			for _, op := range generatedOps() {
				e := manifestEntry{
					Command:    commandPath(op),
					Summary:    op.Summary,
					Mutating:   op.isMutating(),
					Deprecated: op.Deprecated,
					Binary:     op.BinaryContentType != "",
					Paginated:  op.isPaginated(),
				}
				for _, p := range op.PathParams {
					e.Args = append(e.Args, p.Name)
				}
				for _, p := range op.QueryParams {
					e.Flags = append(e.Flags, flagInfo{Name: p.Name, Type: p.Type})
				}
				if op.Body != nil {
					e.Example = op.Body.Example
				}
				manifest = append(manifest, e)
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			return enc.Encode(manifest)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "(siempre JSON)")
	return c
}

// commandPath reconstruye "factuarea <grupos...> <accion>".
func commandPath(op genOp) string {
	parts := append([]string{"factuarea"}, op.Groups...)
	parts = append(parts, op.Action)
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " "
		}
		out += p
	}
	return out
}
```
Register en `root.go`: añade `newCommandsCmd()` al `AddCommand`.

- [ ] **Step 4: Run → PASS. Commit.**
```bash
git add internal/cmd/commands.go internal/cmd/root.go internal/spec/drift_test.go internal/cmd/commands_test.go
git commit -m "feat(cmd): machine-readable commands manifest + spec drift CI guard"
```

---

### Task 8: Integración, smoke real y documentación

**Files:**
- Modify: `README.md` (uso básico)
- Test: smoke manual contra la API real (sandbox)

- [ ] **Step 1: Smoke real (sandbox)**

Run (con una key `fact_test_` real):
```bash
cd /Users/chelu/Personal/factuarea-cli && go build -o factuarea ./cmd/factuarea
export FACTUAREA_API_KEY=fact_test_xxxxxxxxxxxxxxxxxxxxxxxx
./factuarea invoices list --json | head
./factuarea clients list --paginate --json | head
./factuarea invoices get <uuid> --json
./factuarea commands --json | jq 'length'
echo "$?"
```
Expected: listados reales, manifiesto con ~262 comandos, exit codes correctos. Documenta cualquier discrepancia.

- [ ] **Step 2: README mínimo** (instalación temporal `go build`, `login`, ejemplos `invoices list`, `api`, `commands`). Mensajes en español.

- [ ] **Step 3: `make pr`-equivalente final**
```bash
go build ./... && go vet ./... && gofmt -l . && go test ./... -count=1
```
Expected: TODO verde.

- [ ] **Step 4: Commit.**
```bash
git add README.md
git commit -m "docs: README with basic usage; Plan 2 command generation complete"
```

---

## Self-Review (rellenado tras escribir el plan)

**Cobertura:** generación desde openapi (T2-T4) · árbol N-nivel espejando contrato + kebab (T2/T6) · override determinista por operationId + drift-guard (T6/T7) · runtime binario/multipart/paginación/idempotencia (T5) · `commands` manifest agent-first (T7) · SpecHash (T1) · guards `--live` heredados + UsageArgs en generados (T6). **Fuera (trackeado):** scope-check (backend `x-required-scope`), confirmación tipada por irreversibilidad (backend `x-irreversible`), flags tipados por campo (futuro).

**Placeholders:** el código de `libopenapi` (T3) lleva una nota explícita de "ajustar a la API real de la versión" — es una instrucción de compilación, no un placeholder de lógica (la semántica está completa). El resto es código completo.

**Consistencia de tipos:** `spec.Operation`/`Param`/`Body`/`BinaryResponse` (T3) → tabla `genOp`/`genParam`/`genBody` generada (T4) → consumida por el builder (T6) y el manifest (T7). `Resolve` (T2) usado por `Load` (T3). `client.Paginate`/`MultipartBody` (T5) usados por el builder (T6). `cmd.UsageArgs`/`safety.RequireLive`/`output.*`/`newCLIContext` (Plan 1) reutilizados sin duplicar.

**Riesgo principal:** la API concreta de `libopenapi` (T3) — el implementador debe compilar contra la versión instalada y ajustar los accesores (`Schema()`, orderedmaps) manteniendo la semántica; el test contra el spec real es la red de seguridad.
