package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
	"github.com/factuarea/factuarea-cli/internal/spec"
)

// Gate de regresión del BC Automation.
//
// Los 18 comandos del BC son 100% generados desde el spec embebido y ninguno
// tiene test propio, igual que los otros 414: el repo prueba el generador con
// casos representativos y el manifiesto entero. Lo que hoy NADIE impide es que
// una regeneración desde un spec que no conozca el BC —`make generate`, que tira
// del spec de PRODUCCIÓN, donde el epic no está desplegado— borre la superficie
// entera dejando `go build`, `go vet`, `gofmt` y `go test` verdes: el árbol
// simplemente no aparece. `TestSpecOperationParity` tampoco lo caza (compara el
// spec embebido contra producción, es opt-in y va con `continue-on-error`).
//
// Este fichero fija la superficie contra la tabla medida sobre el árbol
// regenerado y falla NOMBRANDO lo que falte o sobre.
//
// Va en DOS mitades complementarias, y ninguna sustituye a la otra:
//   - Las que leen el MANIFIESTO (`commands --json`) cazan una regeneración mala.
//     Ese manifiesto se hornea en el codegen a partir del lado de ENTRADA de cada
//     operación, así que no vuelve a mirar `openapi.json` al arrancar.
//   - Las que leen el SPEC EMBEBIDO (`spec.Raw`) cazan un contrato de RESPUESTA
//     perdido. Por lo anterior, borrar un schema de salida del spec no mueve ni un
//     byte de `resources_gen.go` y dejaría a la primera mitad entera en verde.
//
// Lo que este gate NO congela, a propósito:
//   - `full_replace`: el manifiesto lo publica `true` para `rules update`, pero
//     esa operación es de edición PARCIAL —lo dicen su controlador y el `--help`
//     del propio comando, que nace del mismo `op.isUpdate()`—. Es deuda
//     preexistente y CLI-wide (28 comandos). Congelarla convertiría una
//     afirmación falsa en contrato verificado.

const automationsPrefix = "factuarea automations "

// automationsCommandCount es el suelo y el techo: exactamente 18 operaciones v1
// del BC. Si la tabla de abajo deja de sumar esto, el gate se para antes de
// comparar nada (una tabla mutilada compararía menos de lo que promete).
const automationsCommandCount = 18

// automationsCommand es una fila de la superficie CONGELADA del BC, medida sobre
// `commands --json` del árbol regenerado.
type automationsCommand struct {
	command      string
	scope        string
	args         []string
	paginated    bool
	irreversible bool
}

var automationsSurface = []automationsCommand{
	{command: "factuarea automations catalog show", scope: "automations:read"},
	{command: "factuarea automations catalog trigger-fields", scope: "automations:read", args: []string{"trigger"}},
	{command: "factuarea automations rules list", scope: "automations:read", paginated: true},
	{command: "factuarea automations rules create", scope: "automations:write"},
	{command: "factuarea automations rules show", scope: "automations:read", args: []string{"rule"}},
	{command: "factuarea automations rules update", scope: "automations:write", args: []string{"rule"}},
	{command: "factuarea automations rules delete", scope: "automations:delete", args: []string{"rule"}, irreversible: true},
	{command: "factuarea automations rules activate", scope: "automations:write", args: []string{"rule"}},
	{command: "factuarea automations rules pause", scope: "automations:write", args: []string{"rule"}},
	{command: "factuarea automations rules dry-run", scope: "automations:read", args: []string{"rule"}},
	{command: "factuarea automations rules versions list", scope: "automations:read", args: []string{"rule"}, paginated: true},
	{command: "factuarea automations rules versions show", scope: "automations:read", args: []string{"rule", "version"}},
	{command: "factuarea automations runs list", scope: "automation_runs:read", paginated: true},
	{command: "factuarea automations runs show", scope: "automation_runs:read", args: []string{"run"}},
	{command: "factuarea automations runs replay", scope: "automations:write", args: []string{"run"}, irreversible: true},
	{command: "factuarea automations runs steps list", scope: "automation_runs:read", args: []string{"run"}, paginated: true},
	{command: "factuarea automations runs steps replay", scope: "automations:write", args: []string{"run", "step_index"}, irreversible: true},
	{command: "factuarea automations usage show", scope: "automations:read"},
}

// automationsScopeTally es el reparto de ámbitos congelado. Suma 18: son 8 de
// `automations:read`, no 9 — la tabla del preflight arrastraba un desliz
// aritmético que habría hecho nacer el gate pidiendo una operación inexistente.
var automationsScopeTally = map[string]int{
	"automations:read":     8,
	"automations:write":    6,
	"automations:delete":   1,
	"automation_runs:read": 3,
}

// automationsManifest ejecuta `commands --json` y devuelve SOLO las entradas del
// BC, indexadas por comando.
func automationsManifest(t *testing.T) map[string]map[string]any {
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
		if cmd, _ := e["command"].(string); strings.HasPrefix(cmd, automationsPrefix) {
			got[cmd] = e
		}
	}
	// Suelo anti-vacuidad: sin esto, un manifiesto sin una sola entrada del BC
	// dejaría los conjuntos "sobra" vacíos y varios asserts pasarían recorriendo
	// cero elementos.
	if len(got) == 0 {
		t.Fatalf("el manifiesto (%d comandos en total) no trae NI UNO con el prefijo %q: "+
			"la superficie del BC Automation desapareció entera. Causa típica: se regeneró con "+
			"`make generate` (spec de PRODUCCIÓN, sin el epic) en vez de `make generate-dev`",
			len(manifest), automationsPrefix)
	}
	return got
}

func automationsFrozenNames() map[string]automationsCommand {
	want := make(map[string]automationsCommand, len(automationsSurface))
	for _, c := range automationsSurface {
		want[c.command] = c
	}
	return want
}

// TestAutomationsSurfaceIsExactlyTheFrozenSet — task §4.1.
func TestAutomationsSurfaceIsExactlyTheFrozenSet(t *testing.T) {
	want := automationsFrozenNames()
	if len(want) != automationsCommandCount || len(automationsSurface) != automationsCommandCount {
		t.Fatalf("la tabla congelada tiene %d filas (%d nombres únicos); el BC son %d operaciones v1: "+
			"si la superficie cambió de verdad, actualiza también automationsCommandCount",
			len(automationsSurface), len(want), automationsCommandCount)
	}

	got := automationsManifest(t)

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
		t.Fatalf("la superficie del BC Automation ya no es la congelada (%d comandos esperados, %d en el manifiesto).\n"+
			"  FALTAN (%d): %s\n"+
			"  SOBRAN (%d): %s\n"+
			"Si faltan: la regeneración perdió operaciones (¿`make generate` contra producción?). "+
			"Si sobran: el backend publicó rutas v1 nuevas del BC y hay que ampliar la tabla de este gate.",
			len(want), len(got),
			len(missing), strings.Join(missing, ", "),
			len(extra), strings.Join(extra, ", "))
	}
}

// TestAutomationsRequiredScopes — task §4.2.
func TestAutomationsRequiredScopes(t *testing.T) {
	got := automationsManifest(t)

	tally := map[string]int{}
	for _, want := range automationsSurface {
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

	for scope, n := range automationsScopeTally {
		if tally[scope] != n {
			t.Errorf("reparto de ámbitos: %s aparece %d vez/veces, quiero %d", scope, tally[scope], n)
		}
	}
	for scope, n := range tally {
		if _, declared := automationsScopeTally[scope]; !declared {
			t.Errorf("ámbito no congelado en el BC: %s (%d comandos)", scope, n)
		}
	}

	var total int
	for _, n := range automationsScopeTally {
		total += n
	}
	if total != automationsCommandCount {
		t.Fatalf("el reparto congelado suma %d, y el BC son %d operaciones", total, automationsCommandCount)
	}
}

// TestAutomationsIrreversibleAndPaginatedMarks — task §4.3.
//
// Se asserta en las DOS direcciones: marcado donde toca y NO marcado en el
// resto. Perder `irreversible` desactiva la confirmación de una operación
// destructiva; ganarlo de más rompe a quien ya automatiza el comando.
// Irreversibles son TRES, no una: además del borrado de regla, los dos replays
// (relanzar rearma efectos externos ya emitidos). Paginados son CUATRO, no dos:
// además de los dos listados principales, versiones de regla y pasos de
// ejecución.
func TestAutomationsIrreversibleAndPaginatedMarks(t *testing.T) {
	got := automationsManifest(t)

	var irreversible, paginated []string
	for _, want := range automationsSurface {
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

	if len(irreversible) != 3 {
		sort.Strings(irreversible)
		t.Errorf("el BC declara %d operaciones irreversibles, quiero 3 (borrado de regla + los dos replays): %v",
			len(irreversible), irreversible)
	}
	if len(paginated) != 4 {
		sort.Strings(paginated)
		t.Errorf("el BC declara %d listados paginados, quiero 4 (reglas, ejecuciones, versiones y pasos): %v",
			len(paginated), paginated)
	}
}

// TestAutomationsPositionalArgs — refuerzo de §4.1: un renombrado de parámetro de
// ruta aguas arriba cambia la firma del comando sin quitarlo del conjunto.
func TestAutomationsPositionalArgs(t *testing.T) {
	got := automationsManifest(t)

	for _, want := range automationsSurface {
		e, ok := got[want.command]
		if !ok {
			t.Errorf("%s: no está en el manifiesto", want.command)
			continue
		}
		raw, _ := e["args"].([]any)
		gotArgs := make([]string, 0, len(raw))
		for _, a := range raw {
			s, _ := a.(string)
			gotArgs = append(gotArgs, s)
		}
		wantArgs := want.args
		if wantArgs == nil {
			wantArgs = []string{}
		}
		if strings.Join(gotArgs, ",") != strings.Join(wantArgs, ",") {
			t.Errorf("%s: args = %v, quiero %v", want.command, gotArgs, wantArgs)
		}
	}
}

// TestAutomationsRulesListJSONOutputIsCleanOnStdout — task §4.4.
//
// La salida `--json` tiene que ser parseable por `jq` SIN limpieza previa: los
// mensajes informativos (aquí, el `request_id` de --verbose) van por stderr.
func TestAutomationsRulesListJSONOutputIsCleanOnStdout(t *testing.T) {
	const payload = `{"data":[{"id":"aut_rule_1","name":"Aviso de impago","scope":"empresa"}],"has_more":false,"next_cursor":null}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/account" {
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["automations:read"]}}}`))
			return
		}
		if r.URL.Path != "/v1/automations/rules" {
			t.Errorf("path inesperado: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("X-Request-Id", "req_automations_1")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"automations", "rules", "list", "--json", "--verbose"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v (stderr: %s)", err, stderr.String())
	}

	var parsed map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("la salida estándar no es JSON parseable sin limpieza previa: %v\nstdout: %q", err, stdout.String())
	}
	if _, ok := parsed["data"]; !ok {
		t.Fatalf("la salida no trae el envelope de la API: %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "request_id") {
		t.Errorf("un mensaje informativo se coló en la salida estándar: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "request_id: req_automations_1") {
		t.Errorf("el mensaje informativo debería ir por stderr, y stderr trae: %q", stderr.String())
	}
}

// TestAutomationsRunsListFollowsCursor — task §4.5.
func TestAutomationsRunsListFollowsCursor(t *testing.T) {
	var cursors []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/account" {
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["automation_runs:read"]}}}`))
			return
		}
		if r.URL.Path != "/v1/automations/runs" {
			t.Errorf("path inesperado: %s %s", r.Method, r.URL.Path)
			return
		}
		cursor := r.URL.Query().Get("starting_after")
		cursors = append(cursors, cursor)
		switch cursor {
		case "":
			_, _ = w.Write([]byte(`{"data":[{"id":"aut_run_1"},{"id":"aut_run_2"}],"has_more":true,"next_cursor":"aut_run_2"}`))
		case "aut_run_2":
			_, _ = w.Write([]byte(`{"data":[{"id":"aut_run_3"}],"has_more":false,"next_cursor":null}`))
		default:
			t.Errorf("cursor inesperado: %q", cursor)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"automations", "runs", "list", "--paginate", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v (stderr: %s)", err, stderr.String())
	}

	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
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

	want := []string{"aut_run_1", "aut_run_2", "aut_run_3"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("el listado no devolvió el conjunto completo: got %v, want %v", ids, want)
	}
	if strings.Join(cursors, ",") != ",aut_run_2" {
		t.Fatalf("el CLI no encadenó el cursor: peticiones con starting_after=%v, quiero [\"\" \"aut_run_2\"]", cursors)
	}
}

// TestAutomationsRuleDeleteRefusesWithoutConfirm — task §4.6.
//
// `--skip-scope-check` no es adorno: el orden de guardas es RequireLive →
// comprobación de ámbitos → Confirm, y la comprobación de ámbitos hace
// `GET /v1/account`. Sin ese flag "falla sin tocar la red" sería falso, y el
// servidor que aquí prohíbe cualquier petición recibiría una. Es el patrón ya
// establecido en `build_generated_test.go`.
func TestAutomationsRuleDeleteRefusesWithoutConfirm(t *testing.T) {
	var hits []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"automations", "rules", "delete", "aut_rule_123", "--skip-scope-check", "--no-input"})
	err := root.Execute()

	if err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("esperaba que el borrado se negara pidiendo --confirm, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit code = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
	}
	if len(hits) > 0 {
		t.Fatalf("el borrado sin --confirm NO debe tocar la red, y llegaron: %v", hits)
	}
}

// TestAutomationsPortfolioScopeContractIsFrozen — task §4.7.
//
// Los cuatro puntos que `automation-portfolio-rules` añadió a estos MISMOS
// esquemas v1. Sin congelarlos, una regeneración desde un spec anterior a ese
// change los perdería sin que falle nada: el conjunto de comandos seguiría
// siendo 18 y §4.1-§4.3 seguirían verdes.
//
// Tres son legibles en el manifiesto; el cuarto —el alcance en el recurso de
// regla y el bloque de empresa sujeto en el de ejecución— vive en los schemas de
// RESPUESTA, que el manifiesto no publica, así que se lee del spec embebido.
func TestAutomationsPortfolioScopeContractIsFrozen(t *testing.T) {
	got := automationsManifest(t)

	// (a) alcance como campo tipado del cuerpo, con sus dos valores, en el alta
	// Y en la actualización (el spec lo declara en los dos).
	for _, cmd := range []string{
		"factuarea automations rules create",
		"factuarea automations rules update",
	} {
		e, ok := got[cmd]
		if !ok {
			t.Errorf("%s: no está en el manifiesto", cmd)
			continue
		}
		field := manifestBodyField(e, "scope")
		if field == nil {
			t.Errorf("%s: el cuerpo ya no declara el campo `scope` (alcance de cartera perdido)", cmd)
			continue
		}
		if kind, _ := field["kind"].(string); kind != "scalar" {
			t.Errorf("%s: body_fields.scope.kind = %q, quiero scalar (si deja de ser escalar no se genera flag)", cmd, kind)
		}
		if enum := stringSliceOf(field["enum"]); strings.Join(enum, ",") != "empresa,cartera" {
			t.Errorf("%s: body_fields.scope.enum = %v, quiero [empresa cartera]", cmd, enum)
		}
	}

	// (c) filtro de alcance en el listado de reglas.
	if !manifestHasFlag(got["factuarea automations rules list"], "scope") {
		t.Error("factuarea automations rules list: falta el filtro de consulta `scope` (alcance de cartera)")
	}
	// (d) filtro por empresa sujeto en el listado de ejecuciones.
	if !manifestHasFlag(got["factuarea automations runs list"], "subject_company_id") {
		t.Error("factuarea automations runs list: falta el filtro de consulta `subject_company_id` (empresa sujeto)")
	}

	// (b) el alcance en el recurso de regla y la empresa sujeto en el de
	// ejecución: schemas de respuesta, leídos del spec embebido.
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type       json.RawMessage `json:"type"`
					Enum       []string        `json:"enum"`
					Properties map[string]struct {
						Type json.RawMessage `json:"type"`
					} `json:"properties"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(spec.Raw, &doc); err != nil {
		t.Fatalf("el spec embebido no es JSON: %v", err)
	}

	rule, ok := doc.Components.Schemas["AutomationRule"]
	if !ok {
		t.Fatal("el spec embebido no declara el schema AutomationRule")
	}
	ruleScope, ok := rule.Properties["scope"]
	if !ok {
		t.Error("AutomationRule ya no declara la propiedad `scope`: la salida --json de obtener y listar reglas perdería el alcance")
	} else if strings.Join(ruleScope.Enum, ",") != "empresa,cartera" {
		t.Errorf("AutomationRule.scope.enum = %v, quiero [empresa cartera]", ruleScope.Enum)
	}

	run, ok := doc.Components.Schemas["AutomationRun"]
	if !ok {
		t.Fatal("el spec embebido no declara el schema AutomationRun")
	}
	subject, ok := run.Properties["subject_company"]
	if !ok {
		t.Error("AutomationRun ya no declara el bloque `subject_company`: las ejecuciones de cartera perderían la empresa hija sobre la que actuaron")
	} else {
		for _, prop := range []string{"id", "name"} {
			if _, ok := subject.Properties[prop]; !ok {
				t.Errorf("AutomationRun.subject_company ya no declara `%s`", prop)
			}
		}
	}
}

// automationRunResponseKeys es el conjunto EXACTO de claves de primer nivel del
// recurso de ejecución, medido sobre el spec regenerado. Son 23: las 21 que el BC
// publicaba y las DOS que la corrección de contrato del 2026-08-14 añadió
// —`discard_reason` y `discard_reason_label`—, sin las cuales una ejecución
// `failed`/`dead_lettered`/`blocked` llegaba al integrador con `status` y sin
// NINGÚN motivo (un corte por guardrail ni siquiera deja un paso que lo explique).
//
// Se congela el conjunto exacto, no la mera presencia de las dos nuevas: el modo
// de fallo que importa es el spec embebido RETROCEDIENDO —una regeneración desde
// una rama anterior, o desde producción mientras el epic no esté desplegado—, y
// eso se lleva por delante claves que nadie enumeró. Un `sobra` también es señal:
// significa que el backend amplió el contrato y esta superficie no se ha
// re-propagado, que es exactamente el aviso que hoy no existía y obligó a
// descubrir esta ampliación a mano.
var automationRunResponseKeys = []string{
	"attempts", "automation_rule_id", "chain_depth", "correlation_id", "created_at",
	"discard_reason", "discard_reason_label", "event_aggregate_id", "event_name",
	"event_occurred_on", "event_payload", "finished_at", "id", "last_error", "object",
	"rule_snapshot", "rule_version", "started_at", "status", "steps", "subject_company",
	"trigger_type", "updated_at",
}

// TestAutomationRunResponseContractIsFrozen — el contrato de RESPUESTA de la
// ejecución, leído del spec embebido.
//
// El manifiesto no sirve aquí: `commands --json` publica el lado de ENTRADA de
// cada operación (args, flags, body_fields, ámbito, paginación, irreversibilidad),
// que se hornea en el codegen. Los schemas de respuesta no lo atraviesan, así que
// perder una clave de salida no mueve ni un byte de `resources_gen.go` y las dos
// mitades del gate que miran el manifiesto seguirían verdes. Ésta es la única que
// puede cazarlo.
func TestAutomationRunResponseContractIsFrozen(t *testing.T) {
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type json.RawMessage `json:"type"`
				} `json:"properties"`
				Required []string `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(spec.Raw, &doc); err != nil {
		t.Fatalf("el spec embebido no es JSON: %v", err)
	}

	run, ok := doc.Components.Schemas["AutomationRun"]
	if !ok {
		t.Fatal("el spec embebido no declara el schema AutomationRun: el recurso de ejecución desapareció entero")
	}

	want := append([]string(nil), automationRunResponseKeys...)
	sort.Strings(want)
	got := make([]string, 0, len(run.Properties))
	for k := range run.Properties {
		got = append(got, k)
	}
	sort.Strings(got)

	if diff := missingFrom(want, got); len(diff) > 0 {
		t.Errorf("AutomationRun.properties ya no declara %v: el contrato de respuesta RETROCEDIÓ. "+
			"Causa típica: se regeneró desde un spec anterior a la corrección (o desde producción). "+
			"Regenera con `make generate-dev` contra la rama del epic", diff)
	}
	if diff := missingFrom(got, want); len(diff) > 0 {
		t.Errorf("AutomationRun.properties declara %v, que esta tabla no congela: el backend amplió el "+
			"contrato de la ejecución y la CLI no se había re-propagado. Añádelas a automationRunResponseKeys "+
			"tras comprobar que el spec embebido viene de la rama correcta", diff)
	}

	// Una clave que deja de ser `required` es una pérdida silenciosa para el
	// cliente generado: pasa de "siempre viene" a "puede faltar" sin que el
	// conjunto de propiedades cambie.
	declared := map[string]bool{}
	for _, k := range run.Required {
		declared[k] = true
	}
	for _, k := range want {
		if !declared[k] {
			t.Errorf("AutomationRun.required ya no incluye %q: el cliente dejaría de poder darla por presente", k)
		}
	}

	// Las dos claves del motivo son nullables por contrato: `null` mientras la
	// ejecución no haya terminado en rojo, y la etiqueta también cuando el valor
	// almacenado es anterior al catálogo cerrado y por eso no se puede interpretar.
	for _, k := range []string{"discard_reason", "discard_reason_label"} {
		prop, ok := run.Properties[k]
		if !ok {
			// Su ausencia ya la reporta la comparación de conjuntos de arriba;
			// insistir aquí sólo añadiría un error derivado sobre el mismo hecho.
			continue
		}
		// El spec embebido va indentado (`make normalize-spec`), así que el crudo
		// del tipo trae saltos de línea: se compacta antes de comparar.
		var compact bytes.Buffer
		if err := json.Compact(&compact, prop.Type); err != nil {
			t.Errorf("AutomationRun.%s.type no es JSON: %v", k, err)
			continue
		}
		if compact.String() != `["string","null"]` {
			t.Errorf("AutomationRun.%s.type = %s, quiero [\"string\",\"null\"]: si deja de ser nullable, "+
				"toda ejecución que aún no ha fallado violaría su propio schema", k, compact.String())
		}
	}
}

// missingFrom devuelve los elementos de want que no están en got. Ambos ordenados.
func missingFrom(want, got []string) []string {
	have := make(map[string]bool, len(got))
	for _, k := range got {
		have[k] = true
	}
	var out []string
	for _, k := range want {
		if !have[k] {
			out = append(out, k)
		}
	}
	return out
}

func manifestBodyField(entry map[string]any, name string) map[string]any {
	fields, _ := entry["body_fields"].([]any)
	for _, raw := range fields {
		f, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := f["name"].(string); n == name {
			return f
		}
	}
	return nil
}

func manifestHasFlag(entry map[string]any, name string) bool {
	flags, _ := entry["flags"].([]any)
	for _, raw := range flags {
		f, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if n, _ := f["name"].(string); n == name {
			return true
		}
	}
	return false
}

func stringSliceOf(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}
