package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
	"github.com/factuarea/factuarea-cli/internal/spec"
)

// Gate de regresión de la superficie de proyectos y tareas.
//
// Los 80 comandos de estos siete grupos son 100% generados desde el spec
// embebido y ninguno tiene test propio, como el resto del árbol: el repo prueba
// el generador con casos representativos y el manifiesto entero. Lo que hoy
// nadie impide es que una regeneración desde un spec que aún no conozca la
// superficie —`make generate` descarga el spec PUBLICADO— borre los siete grupos
// dejando `go build`, `go vet`, `gofmt` y `go test` verdes: el árbol
// simplemente no aparece.
//
// Este fichero fija la superficie contra la tabla medida sobre el árbol
// regenerado y falla NOMBRANDO lo que falte o sobre. Va en DOS mitades
// complementarias, y ninguna sustituye a la otra:
//   - Las que leen el MANIFIESTO (`commands --json`) cazan una regeneración mala.
//     Ese manifiesto se hornea en el codegen a partir del lado de ENTRADA de cada
//     operación, así que no vuelve a mirar `openapi.json` al arrancar.
//   - Las que leen el SPEC EMBEBIDO (`spec.Raw`) cazan un contrato de RESPUESTA
//     perdido: borrar un schema de salida no mueve ni un byte de
//     `resources_gen.go` y dejaría a la primera mitad entera en verde.
//
// Los comandos de los siete grupos salen de los `operationId`, sin ningún
// override escrito a mano: `factuarea projects …`, `factuarea tasks …`,
// `factuarea tasks time-entries …`, `factuarea task-labels …`,
// `factuarea task-timers …`, `factuarea users …`, `factuarea notifications …` y
// `factuarea agenda list`.

// tasksGroups son los siete grupos de primer nivel de la superficie.
var tasksGroups = []string{"projects", "tasks", "task-labels", "task-timers", "users", "notifications", "agenda"}

// tasksCommandCount es el suelo y el techo: exactamente 80 operaciones v1, una
// por comando (el generador no agrupa ninguna). Si la tabla de abajo deja de
// sumar esto, el gate se para antes de comparar nada: una tabla mutilada
// compararía menos de lo que promete.
const tasksCommandCount = 80

// tasksGroupTally es el reparto por grupo. Suma 80.
var tasksGroupTally = map[string]int{
	"projects":      22,
	"tasks":         45,
	"task-labels":   5,
	"task-timers":   2,
	"users":         2,
	"notifications": 3,
	"agenda":        1,
}

// tasksCommand es una fila de la superficie CONGELADA, medida sobre
// `commands --json` del árbol regenerado. `flags` son los parámetros de consulta
// que el comando DEBE exponer (comprobación de inclusión, no de igualdad: el
// resto de filtros de un listado los cubre `paginated`).
type tasksCommand struct {
	command      string
	scope        string
	args         []string
	flags        []string
	paginated    bool
	irreversible bool
}

var tasksSurface = []tasksCommand{
	// Agenda
	{command: "factuarea agenda list", scope: "tasks:read", flags: []string{"from", "to", "sources", "assignee_id"}},

	// Avisos
	{command: "factuarea notifications list", scope: "notifications:read", flags: []string{"status", "category"}, paginated: true},
	{command: "factuarea notifications mark-all-read", scope: "notifications:write"},
	{command: "factuarea notifications read", scope: "notifications:write", args: []string{"notification"}},

	// Proyectos
	{command: "factuarea projects list", scope: "projects:read", flags: []string{"include_archived"}, paginated: true},
	{command: "factuarea projects create", scope: "projects:write"},
	{command: "factuarea projects find-by-key", scope: "projects:read"},
	{command: "factuarea projects show", scope: "projects:read", args: []string{"project"}},
	{command: "factuarea projects update", scope: "projects:write", args: []string{"project"}},
	{command: "factuarea projects delete", scope: "projects:delete", args: []string{"project"}, irreversible: true},
	{command: "factuarea projects archive", scope: "projects:write", args: []string{"project"}},
	{command: "factuarea projects unarchive", scope: "projects:write", args: []string{"project"}},
	{command: "factuarea projects columns list", scope: "projects:read", args: []string{"project"}},
	{command: "factuarea projects columns create", scope: "projects:write", args: []string{"project"}},
	{command: "factuarea projects columns reorder", scope: "projects:write", args: []string{"project"}},
	{command: "factuarea projects columns update", scope: "projects:write", args: []string{"project", "column"}},
	// La columna destino de las tareas de una columna borrada viaja en la query.
	{command: "factuarea projects columns delete", scope: "projects:delete", args: []string{"project", "column"}, flags: []string{"move_to_column_id"}, irreversible: true},
	{command: "factuarea projects custom-fields list", scope: "projects:read", args: []string{"project"}},
	{command: "factuarea projects custom-fields create", scope: "projects:write", args: []string{"project"}},
	{command: "factuarea projects custom-fields update", scope: "projects:write", args: []string{"project", "field"}},
	{command: "factuarea projects custom-fields delete", scope: "projects:delete", args: []string{"project", "field"}, irreversible: true},
	{command: "factuarea projects tasks export", scope: "projects:read", args: []string{"project"}},
	{command: "factuarea projects tasks import", scope: "projects:write", args: []string{"project"}},
	{command: "factuarea projects time-invoices create", scope: "invoices:write", args: []string{"project"}},
	{command: "factuarea projects time-invoices preview", scope: "projects:read", args: []string{"project"}},
	{command: "factuarea projects time-summary show", scope: "projects:read", args: []string{"project"}, flags: []string{"from", "to"}},

	// Etiquetas de tarea
	{command: "factuarea task-labels list", scope: "tasks:read", paginated: true},
	{command: "factuarea task-labels create", scope: "tasks:write"},
	{command: "factuarea task-labels show", scope: "tasks:read", args: []string{"label"}},
	{command: "factuarea task-labels update", scope: "tasks:write", args: []string{"label"}},
	{command: "factuarea task-labels delete", scope: "tasks:delete", args: []string{"label"}, irreversible: true},

	// Temporizador del titular de la credencial
	{command: "factuarea task-timers current", scope: "tasks:read"},
	{command: "factuarea task-timers stop", scope: "tasks:write"},

	// Tareas
	{command: "factuarea tasks search", scope: "tasks:read",
		flags:     []string{"q", "project_id", "status", "column_id", "priority", "assignee_id", "label_id", "due_before", "due_after", "completed", "sort"},
		paginated: true},
	{command: "factuarea tasks create", scope: "tasks:write"},
	{command: "factuarea tasks find-by-key", scope: "tasks:read"},
	{command: "factuarea tasks linked", scope: "tasks:read", flags: []string{"entity_type", "entity_id"}, paginated: true},
	{command: "factuarea tasks bulk-delete", scope: "tasks:delete", irreversible: true},
	{command: "factuarea tasks bulk-status", scope: "tasks:write"},
	{command: "factuarea tasks bulk-update", scope: "tasks:write"},
	{command: "factuarea tasks show", scope: "tasks:read", args: []string{"task"}},
	{command: "factuarea tasks update", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks delete", scope: "tasks:delete", args: []string{"task"}, irreversible: true},
	{command: "factuarea tasks duplicate", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks status", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks move", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks reposition", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks assign", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks unassign", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks activities list", scope: "tasks:read", args: []string{"task"}, paginated: true},
	{command: "factuarea tasks attachments list", scope: "tasks:read", args: []string{"task"}},
	{command: "factuarea tasks attachments create", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks attachments show", scope: "tasks:read", args: []string{"task", "attachment"}},
	{command: "factuarea tasks attachments download", scope: "tasks:read", args: []string{"task", "attachment"}},
	{command: "factuarea tasks attachments delete", scope: "tasks:delete", args: []string{"task", "attachment"}, irreversible: true},
	{command: "factuarea tasks comments list", scope: "tasks:read", args: []string{"task"}, paginated: true},
	{command: "factuarea tasks comments create", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks comments update", scope: "tasks:write", args: []string{"task", "comment"}},
	{command: "factuarea tasks comments delete", scope: "tasks:delete", args: []string{"task", "comment"}, irreversible: true},
	{command: "factuarea tasks custom-fields set", scope: "tasks:write", args: []string{"task", "field"}},
	{command: "factuarea tasks entity-links list", scope: "tasks:read", args: []string{"task"}},
	{command: "factuarea tasks entity-links create", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks entity-links delete", scope: "tasks:write", args: []string{"task", "link"}},
	{command: "factuarea tasks external-links list", scope: "tasks:read", args: []string{"task"}},
	{command: "factuarea tasks external-links create", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks external-links delete", scope: "tasks:write", args: []string{"task", "link"}},
	{command: "factuarea tasks labels assign", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks labels unassign", scope: "tasks:write", args: []string{"task", "label"}},
	{command: "factuarea tasks relations list", scope: "tasks:read", args: []string{"task"}},
	{command: "factuarea tasks relations create", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks relations delete", scope: "tasks:write", args: []string{"task", "relation"}},
	{command: "factuarea tasks time-entries list", scope: "tasks:read", args: []string{"task"}, paginated: true},
	{command: "factuarea tasks time-entries create", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks time-entries show", scope: "tasks:read", args: []string{"task", "time_entry"}},
	{command: "factuarea tasks time-entries update", scope: "tasks:write", args: []string{"task", "time_entry"}},
	{command: "factuarea tasks time-entries delete", scope: "tasks:delete", args: []string{"task", "time_entry"}, irreversible: true},
	{command: "factuarea tasks timer start", scope: "tasks:write", args: []string{"task"}},
	{command: "factuarea tasks upload-links create", scope: "tasks:write", args: []string{"task"}},

	// Usuarios asignables
	{command: "factuarea users list", scope: "users:read", flags: []string{"q"}, paginated: true},
	{command: "factuarea users me", scope: "users:read"},
}

// tasksScopeTally es el reparto de ámbitos congelado. Suma 80. El único que no
// es de estos módulos es `invoices:write`, de la factura de horas de un proyecto.
var tasksScopeTally = map[string]int{
	"tasks:read":          18,
	"tasks:write":         29,
	"tasks:delete":        6,
	"projects:read":       8,
	"projects:write":      10,
	"projects:delete":     3,
	"invoices:write":      1,
	"users:read":          2,
	"notifications:read":  1,
	"notifications:write": 2,
}

const (
	// tasksIrreversibleCount: las nueve bajas (proyecto, columna, campo
	// personalizado, etiqueta, tareas en lote, tarea, adjunto, comentario y
	// entrada de tiempo). Las relaciones, los vínculos y las etiquetas asignadas
	// se quitan sin confirmación: se pueden volver a crear.
	tasksIrreversibleCount = 9
	// tasksPaginatedCount: los nueve listados con cursor `starting_after`.
	tasksPaginatedCount = 9
)

// tasksManifest ejecuta `commands --json` y devuelve SOLO las entradas de los
// siete grupos, indexadas por comando.
func tasksManifest(t *testing.T) map[string]map[string]any {
	t.Helper()

	root := NewRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs([]string{"commands", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("commands --json falló: %v (stderr: %s)", err, errOut.String())
	}

	var manifest []map[string]any
	if err := json.Unmarshal(out.Bytes(), &manifest); err != nil {
		t.Fatalf("el manifiesto no es JSON: %v", err)
	}

	got := map[string]map[string]any{}
	for _, e := range manifest {
		cmd, _ := e["command"].(string)
		for _, group := range tasksGroups {
			if strings.HasPrefix(cmd, "factuarea "+group+" ") {
				got[cmd] = e
				break
			}
		}
	}
	// Suelo anti-vacuidad: sin esto, un manifiesto sin una sola entrada de los
	// siete grupos dejaría los conjuntos "sobra" vacíos y varios asserts pasarían
	// recorriendo cero elementos.
	if len(got) == 0 {
		t.Fatalf("el manifiesto (%d comandos en total) no trae NI UN comando de %v: "+
			"la superficie de proyectos y tareas desapareció entera. Causa típica: se regeneró con "+
			"`make generate` (spec publicado, sin esta superficie) en vez de `make generate-dev`",
			len(manifest), tasksGroups)
	}
	return got
}

func tasksFrozenNames() map[string]tasksCommand {
	want := make(map[string]tasksCommand, len(tasksSurface))
	for _, c := range tasksSurface {
		want[c.command] = c
	}
	return want
}

func manifestStringList(e map[string]any, key string) []string {
	raw, _ := e[key].([]any)
	out := make([]string, 0, len(raw))
	for _, a := range raw {
		s, _ := a.(string)
		out = append(out, s)
	}
	return out
}

func manifestFlagNames(e map[string]any) []string {
	flags, _ := e["flags"].([]any)
	out := make([]string, 0, len(flags))
	for _, raw := range flags {
		f, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := f["name"].(string)
		out = append(out, name)
	}
	return out
}

// TestTasksSurfaceIsExactlyTheFrozenSet — la superficie es EXACTAMENTE la tabla.
func TestTasksSurfaceIsExactlyTheFrozenSet(t *testing.T) {
	want := tasksFrozenNames()
	if len(want) != tasksCommandCount || len(tasksSurface) != tasksCommandCount {
		t.Fatalf("la tabla congelada tiene %d filas (%d nombres únicos); la superficie son %d operaciones v1: "+
			"si cambió de verdad, actualiza también tasksCommandCount",
			len(tasksSurface), len(want), tasksCommandCount)
	}

	var declared int
	for _, n := range tasksGroupTally {
		declared += n
	}
	if declared != tasksCommandCount {
		t.Fatalf("el reparto por grupo suma %d y la superficie son %d operaciones", declared, tasksCommandCount)
	}

	got := tasksManifest(t)

	var missing, extra []string
	for name := range want {
		if _, ok := got[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("la superficie de proyectos y tareas ya no es la congelada (%d comandos esperados, %d en el manifiesto).\n"+
			"  FALTAN (%d): %s\n"+
			"  SOBRAN (%d): %s\n"+
			"Si faltan: la regeneración perdió operaciones (¿`make generate` contra el spec publicado?). "+
			"Si sobran: la API publicó rutas v1 nuevas de estos grupos y hay que ampliar la tabla de este gate.",
			len(want), len(got),
			len(missing), strings.Join(missing, ", "),
			len(extra), strings.Join(extra, ", "))
	}

	tally := map[string]int{}
	for name := range got {
		tally[strings.Fields(name)[1]]++
	}
	for group, n := range tasksGroupTally {
		if tally[group] != n {
			t.Errorf("grupo %s: %d comandos en el manifiesto, quiero %d", group, tally[group], n)
		}
	}
}

// TestTasksRequiredScopes — cada comando pide su scope fino y el reparto es el
// congelado.
func TestTasksRequiredScopes(t *testing.T) {
	got := tasksManifest(t)

	tally := map[string]int{}
	for _, want := range tasksSurface {
		e, ok := got[want.command]
		if !ok {
			t.Errorf("%s: no está en el manifiesto", want.command)
			continue
		}
		scope, _ := e["required_scope"].(string)
		if scope != want.scope {
			t.Errorf("%s: required_scope = %q, quiero %q", want.command, scope, want.scope)
			continue
		}
		tally[scope]++
	}

	for scope, n := range tasksScopeTally {
		if tally[scope] != n {
			t.Errorf("reparto de ámbitos: %s aparece %d vez/veces, quiero %d", scope, tally[scope], n)
		}
	}
	for scope, n := range tally {
		if _, declared := tasksScopeTally[scope]; !declared {
			t.Errorf("ámbito no congelado en la superficie: %s (%d comandos)", scope, n)
		}
	}

	var total int
	for _, n := range tasksScopeTally {
		total += n
	}
	if total != tasksCommandCount {
		t.Fatalf("el reparto congelado suma %d, y la superficie son %d operaciones", total, tasksCommandCount)
	}
}

// TestTasksIrreversibleAndPaginatedMarks se asserta en las DOS direcciones:
// marcado donde toca y NO marcado en el resto. Perder `irreversible` desactiva
// la confirmación de una baja; ganarlo de más rompe a quien ya automatiza el
// comando.
func TestTasksIrreversibleAndPaginatedMarks(t *testing.T) {
	got := tasksManifest(t)

	var irreversible, paginated []string
	for _, want := range tasksSurface {
		e, ok := got[want.command]
		if !ok {
			t.Errorf("%s: no está en el manifiesto", want.command)
			continue
		}
		gotIrr, _ := e["irreversible"].(bool)
		if gotIrr != want.irreversible {
			t.Errorf("%s: irreversible = %v, quiero %v", want.command, gotIrr, want.irreversible)
		}
		if gotIrr {
			irreversible = append(irreversible, want.command)
		}
		gotPag, _ := e["paginated"].(bool)
		if gotPag != want.paginated {
			t.Errorf("%s: paginated = %v, quiero %v (paginated ⇔ query param starting_after)", want.command, gotPag, want.paginated)
		}
		if gotPag {
			paginated = append(paginated, want.command)
		}
	}

	if len(irreversible) != tasksIrreversibleCount {
		sort.Strings(irreversible)
		t.Errorf("la superficie declara %d operaciones irreversibles, quiero %d: %v",
			len(irreversible), tasksIrreversibleCount, irreversible)
	}
	if len(paginated) != tasksPaginatedCount {
		sort.Strings(paginated)
		t.Errorf("la superficie declara %d listados paginados, quiero %d: %v",
			len(paginated), tasksPaginatedCount, paginated)
	}
}

// TestTasksPositionalArgsAndQueryFlags — un renombrado de parámetro de ruta o de
// consulta aguas arriba cambia la firma del comando sin quitarlo del conjunto.
func TestTasksPositionalArgsAndQueryFlags(t *testing.T) {
	got := tasksManifest(t)

	for _, want := range tasksSurface {
		e, ok := got[want.command]
		if !ok {
			t.Errorf("%s: no está en el manifiesto", want.command)
			continue
		}

		gotArgs := manifestStringList(e, "args")
		wantArgs := want.args
		if wantArgs == nil {
			wantArgs = []string{}
		}
		if strings.Join(gotArgs, ",") != strings.Join(wantArgs, ",") {
			t.Errorf("%s: args = %v, quiero %v", want.command, gotArgs, wantArgs)
		}

		have := map[string]bool{}
		for _, name := range manifestFlagNames(e) {
			have[name] = true
		}
		for _, flag := range want.flags {
			if !have[flag] {
				t.Errorf("%s: falta el parámetro de consulta --%s", want.command, flag)
			}
		}
		if want.paginated && (!have["limit"] || !have["starting_after"]) {
			t.Errorf("%s: un listado paginado debe exponer --limit y --starting_after", want.command)
		}
	}
}

// TestTasksCreateDeclaresTheEntityLink — el alta de tarea puede vincularse a un
// documento o contacto en la misma petición, con el `entity_link` del cuerpo.
func TestTasksCreateDeclaresTheEntityLink(t *testing.T) {
	got := tasksManifest(t)
	e, ok := got["factuarea tasks create"]
	if !ok {
		t.Fatal("factuarea tasks create: no está en el manifiesto")
	}

	for _, name := range []string{"project-id", "title"} {
		field := manifestBodyField(e, name)
		if field == nil {
			t.Errorf("factuarea tasks create: el cuerpo ya no declara `%s`", name)
			continue
		}
		if required, _ := field["required"].(bool); !required {
			t.Errorf("factuarea tasks create: `%s` debe ser requerido", name)
		}
	}

	linkType := manifestBodyField(e, "entity-link.type")
	if linkType == nil {
		t.Fatal("factuarea tasks create: el cuerpo ya no declara `entity_link.type`")
	}
	wantTypes := []string{"invoice", "quote", "proforma", "delivery_note", "purchase_invoice", "recurring_invoice", "contact", "product", "employee"}
	if enum := stringSliceOf(linkType["enum"]); strings.Join(enum, ",") != strings.Join(wantTypes, ",") {
		t.Errorf("entity_link.type.enum = %v, quiero %v", enum, wantTypes)
	}
	if manifestBodyField(e, "entity-link.id") == nil {
		t.Error("factuarea tasks create: el cuerpo ya no declara `entity_link.id`")
	}
}

// tasksCreateBodyFields es el manifiesto congelado del cuerpo de `tasks create`:
// nombre del flag, tipo de campo y si es requerido. Solo `project-id` y `title`
// lo son. `entity-link.type` y `entity-link.id` lo son DENTRO de `entity_link`,
// pero el objeto es opcional, así que el manifiesto no los anuncia como requeridos.
var tasksCreateBodyFields = []struct {
	name     string
	kind     string
	required bool
}{
	{"project-id", "scalar", true},
	{"title", "scalar", true},
	{"description", "scalar", false},
	{"priority", "scalar", false},
	{"column-id", "scalar", false},
	{"status", "scalar", false},
	{"start-on", "scalar", false},
	{"due-on", "scalar", false},
	{"assignee-id", "scalar", false},
	{"label-ids", "scalar_array", false},
	{"custom-fields", "map", false},
	{"entity-link.type", "scalar", false},
	{"entity-link.id", "scalar", false},
}

// TestTasksCreateManifestRequiredFieldsAreExactlyProjectAndTitle — lo que
// `commands --json` anuncia de `tasks create` es lo que el comando exige: solo
// `project-id` y `title` son requeridos, y el resto del cuerpo, en su orden.
func TestTasksCreateManifestRequiredFieldsAreExactlyProjectAndTitle(t *testing.T) {
	e, ok := tasksManifest(t)["factuarea tasks create"]
	if !ok {
		t.Fatal("factuarea tasks create: no está en el manifiesto")
	}

	fields, _ := e["body_fields"].([]any)
	if len(fields) != len(tasksCreateBodyFields) {
		t.Fatalf("body_fields de tasks create: %d campos, quiero %d", len(fields), len(tasksCreateBodyFields))
	}

	var required []string
	for i, raw := range fields {
		f, _ := raw.(map[string]any)
		want := tasksCreateBodyFields[i]
		name, _ := f["name"].(string)
		kind, _ := f["kind"].(string)
		req, present := f["required"].(bool)
		if name != want.name || kind != want.kind {
			t.Errorf("body_fields[%d] = %s (%s), quiero %s (%s)", i, name, kind, want.name, want.kind)
		}
		if !present || req != want.required {
			t.Errorf("body_fields[%d] %s: required = %v, quiero %v", i, name, req, want.required)
		}
		if req {
			required = append(required, name)
		}
	}
	if strings.Join(required, ",") != "project-id,title" {
		t.Errorf("requeridos anunciados = %v, quiero exactamente [project-id title]", required)
	}
}

// TestTasksCustomFieldValuesTravelAsAMapOfFieldToValue — los valores de los campos
// personalizados se envían como un mapa `id de campo → valor`, no como una lista.
// Con flags cada valor viaja como texto (la API admite texto, número, booleano o
// lista de opciones y normaliza a texto); un valor tipado, una lista de opciones o
// `null` (dejar el campo vacío) se pasan con el cuerpo completo (-d).
func TestTasksCustomFieldValuesTravelAsAMapOfFieldToValue(t *testing.T) {
	const (
		projectID = "0199152d-525d-7000-8000-0000000000a1"
		fieldA    = "0199152d-525d-7000-8000-0000000000f1"
		fieldB    = "0199152d-525d-7000-8000-0000000000f2"
	)

	dryRun := func(t *testing.T, args ...string) map[string]any {
		t.Helper()
		stdout, stderr, err := runTasksCLI(t, append(args, "--dry-run")...)
		if err != nil {
			t.Fatalf("execute: %v (stderr: %s)", err, stderr)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &body); err != nil {
			t.Fatalf("dry-run debe imprimir JSON válido: %q (%v)", stdout, err)
		}
		return body
	}

	t.Run("flag", func(t *testing.T) {
		body := dryRun(t, "tasks", "create", "--project-id", projectID, "--title", "Llamar",
			"--custom-fields", fieldA+"=42", "--custom-fields", fieldB+"=alta")
		fields, ok := body["custom_fields"].(map[string]any)
		if !ok {
			t.Fatalf("custom_fields debe ser un objeto id → valor, y es %T: %v", body["custom_fields"], body["custom_fields"])
		}
		if fmt.Sprint(fields[fieldA]) != "42" || fields[fieldB] != "alta" {
			t.Errorf("valores mal mapeados: %v", fields)
		}
	})

	t.Run("cuerpo completo", func(t *testing.T) {
		body := dryRun(t, "tasks", "create", "-d",
			`{"project_id":"`+projectID+`","title":"Llamar","custom_fields":{"`+fieldA+`":42,"`+fieldB+`":["a","b"]}}`)
		fields, _ := body["custom_fields"].(map[string]any)
		if fields[fieldA] != float64(42) {
			t.Errorf("un número debe conservar su tipo con -d: %v", fields[fieldA])
		}
		if list, _ := fields[fieldB].([]any); len(list) != 2 {
			t.Errorf("una lista de opciones debe llegar como lista con -d: %v", fields[fieldB])
		}
	})

	t.Run("valor de un campo", func(t *testing.T) {
		body := dryRun(t, "tasks", "custom-fields", "set", "task_9", fieldA, "--value", "42")
		if fmt.Sprint(body["value"]) != "42" {
			t.Errorf("--value debe llegar al cuerpo: %v", body)
		}
		cleared := dryRun(t, "tasks", "custom-fields", "set", "task_9", fieldA, "-d", `{"value":null}`)
		if v, present := cleared["value"]; !present || v != nil {
			t.Errorf("`value: null` con -d debe conservarse para vaciar el campo: %v", cleared)
		}
	})
}

// TestTasksCustomFieldsSetDeclaresAUnionValue — el valor de un campo personalizado
// admite texto, número, booleano, lista de opciones o null: el manifiesto y la ayuda
// lo dicen, el flag sigue enviando texto y lo demás va por -d.
func TestTasksCustomFieldsSetDeclaresAUnionValue(t *testing.T) {
	const union = "string|number|boolean|array|null"
	got := tasksManifest(t)

	value := manifestBodyField(got["factuarea tasks custom-fields set"], "value")
	if value == nil {
		t.Fatal("factuarea tasks custom-fields set: el cuerpo ya no declara `value`")
	}
	if value["type"] != union || value["kind"] != "scalar" || value["required"] != true {
		t.Errorf("tasks custom-fields set value = %v, quiero scalar requerido de tipo %q", value, union)
	}

	for _, cmd := range []string{"factuarea projects custom-fields create", "factuarea projects custom-fields update"} {
		field := manifestBodyField(got[cmd], "default-value")
		if field == nil {
			t.Errorf("%s: el cuerpo ya no declara `default-value`", cmd)
			continue
		}
		if field["type"] != union || field["required"] != false {
			t.Errorf("%s default-value = %v, quiero tipo %q y opcional", cmd, field, union)
		}
	}

	stdout, stderr, err := runTasksCLI(t, "tasks", "custom-fields", "set", "--help")
	if err != nil {
		t.Fatalf("help: %v (stderr: %s)", err, stderr)
	}
	for _, want := range []string{
		"--value (" + union + ") requerido",
		"Los flags de tipo unión (--value) envían texto",
		"los valores tipados, las listas y null van por -d/--data-file",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("la ayuda de tasks custom-fields set debe contener %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "--value ()") {
		t.Errorf("la ayuda no debe mostrar un tipo vacío:\n%s", stdout)
	}

	dry := func(args ...string) map[string]any {
		t.Helper()
		out, errOut, err := runTasksCLI(t, append([]string{"tasks", "custom-fields", "set", "task_9", "field_1"}, append(args, "--dry-run")...)...)
		if err != nil {
			t.Fatalf("execute: %v (stderr: %s)", err, errOut)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &body); err != nil {
			t.Fatalf("dry-run debe imprimir JSON válido: %q (%v)", out, err)
		}
		return body
	}
	if v := dry("--value", "42")["value"]; v != "42" {
		t.Errorf("el flag envía texto: --value 42 dio %#v", v)
	}
	if v := dry("-d", `{"value":42}`)["value"]; v != float64(42) {
		t.Errorf("un número va por -d con su tipo: %#v", v)
	}
	if v := dry("-d", `{"value":true}`)["value"]; v != true {
		t.Errorf("un booleano va por -d con su tipo: %#v", v)
	}
	if v, ok := dry("-d", `{"value":["a","b"]}`)["value"].([]any); !ok || len(v) != 2 {
		t.Errorf("una lista va por -d: %#v", v)
	}
	if body := dry("-d", `{"value":null}`); body["value"] != nil || len(body) != 1 {
		t.Errorf("null va por -d y vacía el campo: %v", body)
	}
}

// tasksTestServer levanta un servidor que contesta el `GET /v1/account` de la
// comprobación de scopes y delega el resto en `handle`.
func tasksTestServer(t *testing.T, scopes string, handle func(w http.ResponseWriter, r *http.Request)) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/account" {
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":[` + scopes + `]}}}`))
			return
		}
		handle(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
}

func runTasksCLI(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	root := NewRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errOut.String(), err
}

// TestTasksCreateSendsTheEntityLinkInTheBody — el `entity_link` llega anidado al
// cuerpo, tanto con el cuerpo completo (-d) como con los flags tipados.
func TestTasksCreateSendsTheEntityLinkInTheBody(t *testing.T) {
	const (
		projectID = "0199152d-525d-7000-8000-0000000000a1"
		contactID = "0199152d-525d-7000-8000-0000000000c1"
	)

	cases := []struct {
		name string
		args []string
	}{
		{
			name: "cuerpo completo",
			args: []string{"tasks", "create", "--json", "-d",
				`{"project_id":"` + projectID + `","title":"Llamar al cliente","entity_link":{"type":"contact","id":"` + contactID + `"}}`},
		},
		{
			name: "flags tipados",
			args: []string{"tasks", "create", "--json",
				"--project-id", projectID, "--title", "Llamar al cliente",
				"--entity-link.type", "contact", "--entity-link.id", contactID},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var method, path string
			var body map[string]any
			tasksTestServer(t, `"tasks:write"`, func(w http.ResponseWriter, r *http.Request) {
				method, path = r.Method, r.URL.Path
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &body)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"data":{"id":"0199152d-525d-7000-8000-0000000000b1","object":"task"}}`))
			})

			if _, stderr, err := runTasksCLI(t, tc.args...); err != nil {
				t.Fatalf("execute: %v (stderr: %s)", err, stderr)
			}

			if method != http.MethodPost || path != "/v1/tasks" {
				t.Fatalf("petición inesperada: %s %s", method, path)
			}
			if body["project_id"] != projectID || body["title"] != "Llamar al cliente" {
				t.Errorf("cuerpo sin project_id o title: %v", body)
			}
			link, _ := body["entity_link"].(map[string]any)
			if link["type"] != "contact" || link["id"] != contactID {
				t.Errorf("entity_link mal formado en el cuerpo: %v", body["entity_link"])
			}
		})
	}
}

// TestTasksCreateDryRunNeedsNoEntityLink — el vínculo es opcional: el alta solo
// con los dos requeridos compila el cuerpo sin pedir `--entity-link.*`.
func TestTasksCreateDryRunNeedsNoEntityLink(t *testing.T) {
	const projectID = "0199152d-525d-7000-8000-0000000000a1"

	tasksTestServer(t, `"tasks:write"`, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("la red NO debe tocarse en --dry-run: %s %s", r.Method, r.URL.Path)
	})

	stdout, stderr, err := runTasksCLI(t, "tasks", "create",
		"--project-id", projectID, "--title", "Llamar al cliente", "--dry-run")
	if err != nil {
		t.Fatalf("execute: %v (stderr: %s)", err, stderr)
	}

	var body map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &body); err != nil {
		t.Fatalf("dry-run debe imprimir JSON válido: %q (%v)", stdout, err)
	}
	if body["project_id"] != projectID || body["title"] != "Llamar al cliente" {
		t.Errorf("cuerpo sin project_id o title: %v", body)
	}
	if _, ok := body["entity_link"]; ok {
		t.Errorf("sin flags de vínculo el cuerpo no debe llevar entity_link: %v", body)
	}
}

// TestTasksCreateEntityLinkIsAllOrNothing — `entity_link` se envía completo o se
// omite. Con un solo hijo el servidor responde 422 (`entity_link.type` e
// `entity_link.id` son requeridos si el vínculo viaja), así que el CLI falla antes
// con la misma exigencia y sin tocar la red.
func TestTasksCreateEntityLinkIsAllOrNothing(t *testing.T) {
	const (
		projectID = "0199152d-525d-7000-8000-0000000000a1"
		contactID = "0199152d-525d-7000-8000-0000000000c1"
	)
	base := []string{"tasks", "create", "--project-id", projectID, "--title", "Llamar al cliente"}

	cases := []struct {
		name        string
		extra       []string
		wantMissing string
	}{
		{"solo el tipo", []string{"--entity-link.type", "contact"}, "--entity-link.id"},
		{"solo el id", []string{"--entity-link.id", contactID}, "--entity-link.type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tasksTestServer(t, `"tasks:write"`, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("un vínculo incompleto NO debe tocar la red: %s %s", r.Method, r.URL.Path)
			})

			args := append(append([]string{}, base...), tc.extra...)
			_, _, err := runTasksCLI(t, append(args, "--dry-run")...)
			if err == nil {
				t.Fatal("un vínculo incompleto debe rechazarse")
			}
			if exit.ForError(err) != exit.Usage {
				t.Errorf("exit code = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
			}
			if want := "faltan campos requeridos: " + tc.wantMissing + " ("; !strings.Contains(err.Error(), want) {
				t.Errorf("mensaje = %q, quiero que contenga %q", err.Error(), want)
			}
		})
	}

	t.Run("completo", func(t *testing.T) {
		stdout, stderr, err := runTasksCLI(t, append(append([]string{}, base...),
			"--entity-link.type", "contact", "--entity-link.id", contactID, "--dry-run")...)
		if err != nil {
			t.Fatalf("execute: %v (stderr: %s)", err, stderr)
		}
		var body struct {
			EntityLink map[string]any `json:"entity_link"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &body); err != nil {
			t.Fatalf("dry-run debe imprimir JSON válido: %q (%v)", stdout, err)
		}
		if body.EntityLink["type"] != "contact" || body.EntityLink["id"] != contactID {
			t.Errorf("entity_link mal formado: %v", body.EntityLink)
		}
	})
}

// TestTasksCreateHelpMarksOnlyTheRealRequiredFields — la ayuda no presenta como
// requeridos los hijos del vínculo opcional.
func TestTasksCreateHelpMarksOnlyTheRealRequiredFields(t *testing.T) {
	stdout, stderr, err := runTasksCLI(t, "tasks", "create", "--help")
	if err != nil {
		t.Fatalf("help: %v (stderr: %s)", err, stderr)
	}

	seen := map[string]bool{}
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "--") {
			continue
		}
		flag := fields[0]
		seen[flag] = true
		wantRequired := flag == "--project-id" || flag == "--title"
		if got := strings.Contains(line, "requerido"); got != wantRequired {
			t.Errorf("ayuda de %s: requerido = %v, quiero %v (%q)", flag, got, wantRequired, line)
		}
	}
	for _, flag := range []string{"--project-id", "--title", "--entity-link.type", "--entity-link.id"} {
		if !seen[flag] {
			t.Errorf("la ayuda de tasks create no lista %s", flag)
		}
	}
}

// TestTasksSearchFollowsCursorAndSendsFilters — el listado paginado encadena el
// cursor y cada petición lleva los filtros.
func TestTasksSearchFollowsCursorAndSendsFilters(t *testing.T) {
	var cursors, projects, statuses []string

	tasksTestServer(t, `"tasks:read"`, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tasks" {
			t.Errorf("path inesperado: %s %s", r.Method, r.URL.Path)
			return
		}
		q := r.URL.Query()
		cursors = append(cursors, q.Get("starting_after"))
		projects = append(projects, q.Get("project_id"))
		statuses = append(statuses, q.Get("status"))
		switch q.Get("starting_after") {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"id":"task_1"},{"id":"task_2"}],"has_more":true,"next_cursor":"task_2"}`))
		case "task_2":
			_, _ = w.Write([]byte(`{"data":[{"id":"task_3"}],"has_more":false,"next_cursor":null}`))
		default:
			t.Errorf("cursor inesperado: %q", q.Get("starting_after"))
		}
	})

	stdout, stderr, err := runTasksCLI(t, "tasks", "search", "--paginate", "--json",
		"--project_id", "proj_1", "--status", "active")
	if err != nil {
		t.Fatalf("execute: %v (stderr: %s)", err, stderr)
	}

	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var item struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			t.Fatalf("línea NDJSON no parseable (%q): %v", line, err)
		}
		ids = append(ids, item.ID)
	}

	if want := "task_1,task_2,task_3"; strings.Join(ids, ",") != want {
		t.Fatalf("el listado no devolvió el conjunto completo: got %v, want %s", ids, want)
	}
	if strings.Join(cursors, ",") != ",task_2" {
		t.Fatalf("el CLI no encadenó el cursor: starting_after=%v, quiero [\"\" \"task_2\"]", cursors)
	}
	for i := range cursors {
		if projects[i] != "proj_1" || statuses[i] != "active" {
			t.Errorf("la petición %d perdió un filtro: project_id=%q status=%q", i, projects[i], statuses[i])
		}
	}
}

// TestTasksTimeEntriesListNestsUnderTheTask — el subrecurso anidado cuelga de la
// tarea y pagina como el resto.
func TestTasksTimeEntriesListNestsUnderTheTask(t *testing.T) {
	var paths []string

	tasksTestServer(t, `"tasks:read"`, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		if r.URL.Query().Get("starting_after") == "" {
			_, _ = w.Write([]byte(`{"data":[{"id":"te_1"}],"has_more":true,"next_cursor":"te_1"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"te_2"}],"has_more":false,"next_cursor":null}`))
	})

	stdout, stderr, err := runTasksCLI(t, "tasks", "time-entries", "list", "task_9", "--paginate", "--json")
	if err != nil {
		t.Fatalf("execute: %v (stderr: %s)", err, stderr)
	}
	if !strings.Contains(stdout, "te_1") || !strings.Contains(stdout, "te_2") {
		t.Errorf("faltan entradas de tiempo en la salida: %q", stdout)
	}
	want := "/v1/tasks/task_9/time-entries?,/v1/tasks/task_9/time-entries?starting_after=te_1"
	if strings.Join(paths, ",") != want {
		t.Errorf("peticiones = %v, quiero %s", paths, want)
	}
}

// TestTasksTimeEntriesCreateSendsThePeriod — la imputación de tiempo manda el
// periodo cerrado en el cuerpo y nunca lo deduce.
func TestTasksTimeEntriesCreateSendsThePeriod(t *testing.T) {
	var method, path, idempotency string
	var body map[string]any

	tasksTestServer(t, `"tasks:write"`, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		idempotency = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"te_1","object":"task_time_entry"}}`))
	})

	_, stderr, err := runTasksCLI(t, "tasks", "time-entries", "create", "task_9", "--json",
		"--started-at", "2026-09-28T09:00:00+02:00",
		"--ended-at", "2026-09-28T10:30:00+02:00",
		"--description", "Revisión del presupuesto",
		"--billable=false")
	if err != nil {
		t.Fatalf("execute: %v (stderr: %s)", err, stderr)
	}

	if method != http.MethodPost || path != "/v1/tasks/task_9/time-entries" {
		t.Fatalf("petición inesperada: %s %s", method, path)
	}
	if body["started_at"] != "2026-09-28T09:00:00+02:00" || body["ended_at"] != "2026-09-28T10:30:00+02:00" {
		t.Errorf("el periodo no llegó intacto al cuerpo: %v", body)
	}
	if body["description"] != "Revisión del presupuesto" || body["billable"] != false {
		t.Errorf("descripción o facturable perdidos: %v", body)
	}
	if idempotency == "" {
		t.Error("la escritura debe llevar Idempotency-Key")
	}
}

// TestTaskTimersCurrentReturnsNullWithoutATimer — sin temporizador en marcha la
// API responde `data: null` y el CLI lo entrega tal cual, con éxito.
func TestTaskTimersCurrentReturnsNullWithoutATimer(t *testing.T) {
	tasksTestServer(t, `"tasks:read"`, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/task-timers/current" {
			t.Errorf("petición inesperada: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":null}`))
	})

	stdout, stderr, err := runTasksCLI(t, "task-timers", "current", "--json")
	if err != nil {
		t.Fatalf("execute: %v (stderr: %s)", err, stderr)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("la salida no es JSON parseable: %v\nstdout: %q", err, stdout)
	}
	data, ok := parsed["data"]
	if !ok || string(bytes.TrimSpace(data)) != "null" {
		t.Errorf("la salida debe conservar `data: null`, y trae %q", stdout)
	}
}

// TestTasksDeletesRefuseWithoutConfirm — las bajas irreversibles se niegan sin
// `--confirm` y NO tocan la red.
//
// `--skip-scope-check` no es adorno: el orden de guardas es RequireLive →
// comprobación de ámbitos → Confirm, y la comprobación de ámbitos hace
// `GET /v1/account`. Sin ese flag "falla sin tocar la red" sería falso.
func TestTasksDeletesRefuseWithoutConfirm(t *testing.T) {
	cases := [][]string{
		{"tasks", "delete", "task_9"},
		{"tasks", "bulk-delete", "--task-ids", "task_1,task_2"},
		{"projects", "delete", "proj_1"},
		{"projects", "columns", "delete", "proj_1", "col_1"},
		{"task-labels", "delete", "label_1"},
	}

	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var hits []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits = append(hits, r.Method+" "+r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":{}}`))
			}))
			t.Cleanup(srv.Close)
			t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
			t.Setenv("FACTUAREA_BASE_URL", srv.URL)

			_, _, err := runTasksCLI(t, append(args, "--skip-scope-check", "--no-input")...)
			if err == nil || !strings.Contains(err.Error(), "--confirm") {
				t.Fatalf("esperaba que la baja se negara pidiendo --confirm, got %v", err)
			}
			if exit.ForError(err) != exit.Usage {
				t.Fatalf("exit code = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
			}
			if len(hits) > 0 {
				t.Fatalf("la baja sin --confirm NO debe tocar la red, y llegaron: %v", hits)
			}
		})
	}
}

// TestProjectsColumnsDeleteSendsTheMoveTarget — al borrar una columna con
// tareas, `move_to_column_id` viaja en la query, junto a la clave de
// idempotencia que exige la baja.
func TestProjectsColumnsDeleteSendsTheMoveTarget(t *testing.T) {
	var method, path, moveTo, idempotency string

	tasksTestServer(t, `"projects:delete"`, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		moveTo = r.URL.Query().Get("move_to_column_id")
		idempotency = r.Header.Get("Idempotency-Key")
		_, _ = w.Write([]byte(`{"data":{"id":"col_1","object":"project_column","deleted":true}}`))
	})

	_, stderr, err := runTasksCLI(t, "projects", "columns", "delete", "proj_1", "col_1",
		"--move_to_column_id", "col_2", "--confirm", "col_1", "--json")
	if err != nil {
		t.Fatalf("execute: %v (stderr: %s)", err, stderr)
	}

	if method != http.MethodDelete || path != "/v1/projects/proj_1/columns/col_1" {
		t.Fatalf("petición inesperada: %s %s", method, path)
	}
	if moveTo != "col_2" {
		t.Errorf("move_to_column_id = %q, quiero col_2", moveTo)
	}
	if idempotency == "" {
		t.Error("la baja irreversible debe llevar Idempotency-Key")
	}
}

// TestProjectsColumnsDeleteWithoutTargetOmitsTheQuery — sin destino no se manda
// la query: la API decide (409 si la columna aún tiene tareas).
func TestProjectsColumnsDeleteWithoutTargetOmitsTheQuery(t *testing.T) {
	var rawQuery string
	var seen bool

	tasksTestServer(t, `"projects:delete"`, func(w http.ResponseWriter, r *http.Request) {
		seen = true
		rawQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"data":{"id":"col_1","object":"project_column","deleted":true}}`))
	})

	_, stderr, err := runTasksCLI(t, "projects", "columns", "delete", "proj_1", "col_1", "--confirm", "col_1", "--json")
	if err != nil {
		t.Fatalf("execute: %v (stderr: %s)", err, stderr)
	}
	if !seen {
		t.Fatal("la petición no llegó al servidor")
	}
	if rawQuery != "" {
		t.Errorf("sin --move_to_column_id la query debe ir vacía, y trae %q", rawQuery)
	}
}

// TestTasksDeleteSendsAnIdempotencyKey — las dos bajas de tareas exigen la
// cabecera y el cliente la genera cuando no se pasa.
func TestTasksDeleteSendsAnIdempotencyKey(t *testing.T) {
	cases := []struct {
		name string
		args []string
		path string
	}{
		{"delete", []string{"tasks", "delete", "task_9", "--confirm", "task_9", "--json"}, "/v1/tasks/task_9"},
		{"bulk-delete", []string{"tasks", "bulk-delete", "--task-ids", "task_1,task_2", "--confirm", "bulk-delete", "--json"}, "/v1/tasks/bulk-delete"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var path, idempotency string

			tasksTestServer(t, `"tasks:delete"`, func(w http.ResponseWriter, r *http.Request) {
				path = r.URL.Path
				idempotency = r.Header.Get("Idempotency-Key")
				_, _ = w.Write([]byte(`{"data":{"deleted":true}}`))
			})

			if _, stderr, err := runTasksCLI(t, tc.args...); err != nil {
				t.Fatalf("execute: %v (stderr: %s)", err, stderr)
			}
			if path != tc.path {
				t.Errorf("path = %q, quiero %q", path, tc.path)
			}
			if idempotency == "" {
				t.Error("la baja debe llevar Idempotency-Key")
			}
		})
	}
}

// tasksSchemaKeys son las claves de primer nivel de los recursos de respuesta,
// medidas sobre el spec regenerado. Se congela el conjunto EXACTO, no la mera
// presencia de algunas: el modo de fallo que importa es el spec embebido
// RETROCEDIENDO —una regeneración desde un spec anterior—, y eso se lleva por
// delante claves que nadie enumeró. Un `sobra` también es señal: la API amplió el
// contrato y la CLI no se ha re-propagado.
var tasksSchemaKeys = map[string][]string{
	"Task": {
		"assignee_id", "column_id", "completed_at", "counts", "created_at", "custom_fields",
		"description", "due_on", "id", "key", "labels", "number", "object", "priority",
		"project_id", "start_on", "status", "title", "updated_at",
	},
	"Project": {
		"archived_at", "billing_contact_id", "billing_hourly_rate", "billing_product_id",
		"created_at", "description", "icon", "id", "is_archived", "key", "name", "object",
		"position", "stats", "updated_at",
	},
}

// tasksNullableKeys son las claves que el contrato declara nullables
// (`["string","null"]`): si dejan de serlo, todo recurso que aún no tenga ese
// dato violaría su propio schema.
var tasksNullableKeys = map[string][]string{
	"Task":    {"assignee_id", "column_id", "completed_at", "description", "due_on", "start_on"},
	"Project": {"archived_at", "billing_contact_id", "billing_hourly_rate", "billing_product_id", "description"},
}

// tasksEnums son los vocabularios cerrados de los recursos de respuesta.
var tasksEnums = map[string]map[string][]string{
	"Task": {
		"object":   {"task"},
		"priority": {"none", "low", "medium", "high", "urgent"},
		"status":   {"planned", "active", "archived"},
	},
	"Project": {
		"object": {"project"},
	},
}

// TestTaskAndProjectResponseSchemasAreFrozen — el contrato de RESPUESTA de las
// dos entidades, leído del spec embebido.
//
// El manifiesto no sirve aquí: `commands --json` publica el lado de ENTRADA de
// cada operación. Los schemas de respuesta no lo atraviesan, así que perder una
// clave de salida no mueve ni un byte de `resources_gen.go` y las mitades del
// gate que miran el manifiesto seguirían verdes. Ésta es la que puede cazarlo.
func TestTaskAndProjectResponseSchemasAreFrozen(t *testing.T) {
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type json.RawMessage `json:"type"`
					Enum []any           `json:"enum"`
				} `json:"properties"`
				Required []string `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(spec.Raw, &doc); err != nil {
		t.Fatalf("el spec embebido no es JSON: %v", err)
	}

	for name, keys := range tasksSchemaKeys {
		schema, ok := doc.Components.Schemas[name]
		if !ok {
			t.Errorf("el spec embebido no declara el schema %s: el recurso desapareció entero", name)
			continue
		}

		want := append([]string(nil), keys...)
		sort.Strings(want)
		got := make([]string, 0, len(schema.Properties))
		for k := range schema.Properties {
			got = append(got, k)
		}
		sort.Strings(got)

		if diff := missingFrom(want, got); len(diff) > 0 {
			t.Errorf("%s.properties ya no declara %v: el contrato de respuesta RETROCEDIÓ. "+
				"Causa típica: se regeneró desde un spec anterior. Regenera con `make generate-dev` "+
				"contra un backend que ya incluya proyectos y tareas", name, diff)
		}
		if diff := missingFrom(got, want); len(diff) > 0 {
			t.Errorf("%s.properties declara %v, que esta tabla no congela: la API amplió el contrato y "+
				"la CLI no se había re-propagado. Añádelas a tasksSchemaKeys tras comprobar que el spec "+
				"embebido viene del backend correcto", name, diff)
		}

		// Una clave que deja de ser `required` es una pérdida silenciosa para el
		// cliente generado: pasa de "siempre viene" a "puede faltar" sin que el
		// conjunto de propiedades cambie.
		required := map[string]bool{}
		for _, k := range schema.Required {
			required[k] = true
		}
		for _, k := range want {
			if !required[k] {
				t.Errorf("%s.required ya no incluye %q: el cliente dejaría de poder darla por presente", name, k)
			}
		}

		for _, k := range tasksNullableKeys[name] {
			prop, ok := schema.Properties[k]
			if !ok {
				continue // su ausencia ya la reporta la comparación de conjuntos
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, prop.Type); err != nil {
				t.Errorf("%s.%s.type no es JSON: %v", name, k, err)
				continue
			}
			if !strings.Contains(compact.String(), `"null"`) {
				t.Errorf("%s.%s.type = %s: debe seguir admitiendo null", name, k, compact.String())
			}
		}

		for k, wantEnum := range tasksEnums[name] {
			prop, ok := schema.Properties[k]
			if !ok {
				continue
			}
			if gotEnum := stringEnumOf(prop.Enum); strings.Join(gotEnum, ",") != strings.Join(wantEnum, ",") {
				t.Errorf("%s.%s.enum = %v, quiero %v", name, k, gotEnum, wantEnum)
			}
		}
	}
}
