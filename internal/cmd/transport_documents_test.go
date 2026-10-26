package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
	"github.com/factuarea/factuarea-cli/internal/spec"
)

// Gate de regresión del DeCA (documento electrónico de control administrativo del
// transporte de mercancías por carretera), el recurso `transport_documents` de la
// API v1.
//
// Los 17 comandos de `factuarea transport-documents …` salen de los
// `operationId` del spec embebido, sin ningún override escrito a mano, igual que
// el resto del árbol. Este fichero fija lo que el generador debe seguir
// derivando del spec para ellos:
//   - la superficie exacta, con su scope fino (`x-required-scope`);
//   - las cinco operaciones irreversibles (`x-irreversible`: emitir, modificar,
//     anular, sustituir y enviar), que se niegan sin `--confirm <id>` y sin tocar
//     la red;
//   - las tres descargas binarias (PDF, QR y justificante de emisión), que se
//     escriben tal cual al fichero de `-o` sin pasar por el parser de JSON;
//   - el contrato de RESPUESTA del recurso, leído del spec embebido, porque
//     perder una clave de salida no mueve ni un byte de `resources_gen.go`.
//
// El listado se llama `index` (no `list`) porque así lo nombra el `operationId`
// del backend (`transport_documents.index`), como `stores index`.

const transportDocumentsPrefix = "factuarea transport-documents "

// transportDocumentsCommandCount es el suelo y el techo: exactamente 17
// operaciones v1, una por comando.
const transportDocumentsCommandCount = 17

// transportDocumentArgs es el único argumento posicional de las operaciones sobre
// un documento: su `id`.
var transportDocumentArgs = []string{"transport_document"}

type transportDocumentsCommand struct {
	command      string
	scope        string
	args         []string
	flags        []string
	paginated    bool
	irreversible bool
	binary       bool
}

var transportDocumentsSurface = []transportDocumentsCommand{
	{command: "factuarea transport-documents index", scope: "transport_documents:read",
		flags:     []string{"status", "status[in]", "transport_date[gte]", "transport_date[lte]", "company_role", "carrier_tax_id", "delivery_note_id", "q", "sort"},
		paginated: true},
	{command: "factuarea transport-documents statuses", scope: "transport_documents:read"},
	{command: "factuarea transport-documents show", scope: "transport_documents:read", args: transportDocumentArgs},
	{command: "factuarea transport-documents create", scope: "transport_documents:write"},
	{command: "factuarea transport-documents update", scope: "transport_documents:write", args: transportDocumentArgs},
	// Solo borra borradores: no consume número ni deja fichero, y el spec no la
	// marca irreversible.
	{command: "factuarea transport-documents delete", scope: "transport_documents:delete", args: transportDocumentArgs},
	{command: "factuarea transport-documents issue", scope: "transport_documents:transition", args: transportDocumentArgs, irreversible: true},
	{command: "factuarea transport-documents modify", scope: "transport_documents:transition", args: transportDocumentArgs, irreversible: true},
	// Cerrar y reabrir se deshacen entre sí dentro del plazo: sin confirmación.
	{command: "factuarea transport-documents close", scope: "transport_documents:transition", args: transportDocumentArgs},
	{command: "factuarea transport-documents reopen", scope: "transport_documents:transition", args: transportDocumentArgs},
	{command: "factuarea transport-documents cancel", scope: "transport_documents:transition", args: transportDocumentArgs, irreversible: true},
	{command: "factuarea transport-documents supersede", scope: "transport_documents:transition", args: transportDocumentArgs, irreversible: true},
	{command: "factuarea transport-documents send", scope: "transport_documents:write", args: transportDocumentArgs, irreversible: true},
	{command: "factuarea transport-documents pdf", scope: "pdfs:read", args: transportDocumentArgs, flags: []string{"version", "download"}, binary: true},
	{command: "factuarea transport-documents qr", scope: "transport_documents:read", args: transportDocumentArgs, flags: []string{"format", "download"}, binary: true},
	// El registro de emisión es JSON; su justificante en PDF es la descarga de abajo.
	{command: "factuarea transport-documents evidence", scope: "transport_documents:read", args: transportDocumentArgs},
	// Lleva el registro de emisión: pide `transport_documents:read`, no `pdfs:read`.
	{command: "factuarea transport-documents issuance-proof", scope: "transport_documents:read", args: transportDocumentArgs, flags: []string{"download"}, binary: true},
}

// transportDocumentsScopeTally es el reparto de ámbitos congelado. Suma 17.
var transportDocumentsScopeTally = map[string]int{
	"transport_documents:read":       6,
	"transport_documents:write":      3,
	"transport_documents:transition": 6,
	"transport_documents:delete":     1,
	"pdfs:read":                      1,
}

const (
	transportDocumentsIrreversibleCount = 5
	transportDocumentsBinaryCount       = 3
	transportDocumentsPaginatedCount    = 1
)

// transportDocumentsManifest ejecuta `commands --json` y devuelve SOLO las
// entradas del recurso, indexadas por comando.
func transportDocumentsManifest(t *testing.T) map[string]map[string]any {
	t.Helper()

	stdout, stderr, err := runTasksCLI(t, "commands", "--json")
	if err != nil {
		t.Fatalf("commands --json falló: %v (stderr: %s)", err, stderr)
	}
	var manifest []map[string]any
	if err := json.Unmarshal([]byte(stdout), &manifest); err != nil {
		t.Fatalf("el manifiesto no es JSON: %v", err)
	}

	got := map[string]map[string]any{}
	for _, e := range manifest {
		if cmd, _ := e["command"].(string); strings.HasPrefix(cmd, transportDocumentsPrefix) {
			got[cmd] = e
		}
	}
	if len(got) == 0 {
		t.Fatalf("el manifiesto (%d comandos en total) no trae NI UN comando de transport-documents: "+
			"el DeCA desapareció entero. Causa típica: se regeneró desde un spec anterior a la release que lo publicó",
			len(manifest))
	}
	return got
}

// TestTransportDocumentsSurfaceIsExactlyTheFrozenSet — la superficie es
// EXACTAMENTE la tabla, y cada comando pide su scope fino.
func TestTransportDocumentsSurfaceIsExactlyTheFrozenSet(t *testing.T) {
	want := make(map[string]transportDocumentsCommand, len(transportDocumentsSurface))
	for _, c := range transportDocumentsSurface {
		want[c.command] = c
	}
	if len(want) != transportDocumentsCommandCount || len(transportDocumentsSurface) != transportDocumentsCommandCount {
		t.Fatalf("la tabla congelada tiene %d filas (%d nombres únicos); la superficie son %d operaciones v1",
			len(transportDocumentsSurface), len(want), transportDocumentsCommandCount)
	}

	got := transportDocumentsManifest(t)

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
		t.Fatalf("la superficie del DeCA ya no es la congelada.\n  FALTAN (%d): %s\n  SOBRAN (%d): %s\n"+
			"Si faltan: la regeneración perdió operaciones. Si sobran: la API publicó rutas v1 nuevas del recurso "+
			"y hay que ampliar la tabla de este gate.",
			len(missing), strings.Join(missing, ", "), len(extra), strings.Join(extra, ", "))
	}

	tally := map[string]int{}
	for _, c := range transportDocumentsSurface {
		scope, _ := got[c.command]["required_scope"].(string)
		if scope != c.scope {
			t.Errorf("%s: required_scope = %q, quiero %q", c.command, scope, c.scope)
			continue
		}
		tally[scope]++
	}
	var total int
	for scope, n := range transportDocumentsScopeTally {
		total += n
		if tally[scope] != n {
			t.Errorf("reparto de ámbitos: %s aparece %d vez/veces, quiero %d", scope, tally[scope], n)
		}
	}
	if total != transportDocumentsCommandCount {
		t.Fatalf("el reparto congelado suma %d, y la superficie son %d operaciones", total, transportDocumentsCommandCount)
	}
}

// TestTransportDocumentsMarksArgsAndFlags — irreversible, binaria y paginada se
// assertan en las DOS direcciones: perder `irreversible` quita la confirmación de
// una emisión; ganarlo de más rompe a quien ya automatiza el comando.
func TestTransportDocumentsMarksArgsAndFlags(t *testing.T) {
	got := transportDocumentsManifest(t)

	var irreversible, binary, paginated []string
	for _, want := range transportDocumentsSurface {
		e, ok := got[want.command]
		if !ok {
			t.Errorf("%s: no está en el manifiesto", want.command)
			continue
		}
		for _, mark := range []struct {
			key  string
			want bool
			into *[]string
		}{
			{"irreversible", want.irreversible, &irreversible},
			{"binary", want.binary, &binary},
			{"paginated", want.paginated, &paginated},
		} {
			value, _ := e[mark.key].(bool)
			if value != mark.want {
				t.Errorf("%s: %s = %v, quiero %v", want.command, mark.key, value, mark.want)
			}
			if value {
				*mark.into = append(*mark.into, want.command)
			}
		}

		wantArgs := want.args
		if wantArgs == nil {
			wantArgs = []string{}
		}
		gotArgs := manifestStringList(e, "args")
		if opt, _ := e["optional_args"].(map[string]any); opt["company"] == "--company" && len(gotArgs) > 0 && gotArgs[0] == "company" {
			gotArgs = gotArgs[1:]
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
	}

	if len(irreversible) != transportDocumentsIrreversibleCount {
		t.Errorf("irreversibles = %v, quiero %d", irreversible, transportDocumentsIrreversibleCount)
	}
	if len(binary) != transportDocumentsBinaryCount {
		t.Errorf("binarias = %v, quiero %d", binary, transportDocumentsBinaryCount)
	}
	if len(paginated) != transportDocumentsPaginatedCount {
		t.Errorf("paginadas = %v, quiero %d", paginated, transportDocumentsPaginatedCount)
	}
}

// transportDocumentsIrreversibleCases son las cinco operaciones irreversibles con
// los campos requeridos de su cuerpo, para que lo único que falte sea confirmar.
var transportDocumentsIrreversibleCases = []struct {
	action string
	scope  string
	extra  []string
}{
	{"issue", "transport_documents:transition", nil},
	{"modify", "transport_documents:transition", []string{"--reason", "Cambio de matrícula", "--data-origin", "company", "--vehicle.tractor-plate", "5678FGH"}},
	{"cancel", "transport_documents:transition", []string{"--reason", "El cliente cancela el pedido"}},
	{"supersede", "transport_documents:transition", []string{"--reference", "DC-TRANSP-2026-0042", "--reason", "Lo sustituye el documento del transportista"}},
	{"send", "transport_documents:write", []string{"--to", "transporte@cliente.example"}},
}

// TestTransportDocumentsIrreversibleRefuseWithoutConfirm — sin `--confirm`, o con
// un `--confirm` que no es el id del documento, la operación se niega y NO toca la
// red. `--skip-scope-check` hace falta para que "sin red" sea cierto: el orden de
// guardas es RequireLive → comprobación de ámbitos (`GET /v1/account`) → Confirm.
func TestTransportDocumentsIrreversibleRefuseWithoutConfirm(t *testing.T) {
	const id = "0199f3a2-525d-7000-8000-0000000000d1"

	for _, tc := range transportDocumentsIrreversibleCases {
		for _, confirm := range []struct {
			name string
			args []string
			want string
		}{
			{"sin --confirm", nil, "--confirm=" + id},
			{"con otro id", []string{"--confirm", "0199f3a2-525d-7000-8000-0000000000d2"}, "no coincide con"},
		} {
			t.Run(tc.action+" "+confirm.name, func(t *testing.T) {
				tasksTestServer(t, `"`+tc.scope+`"`, func(w http.ResponseWriter, r *http.Request) {
					t.Errorf("una operación irreversible sin confirmar NO debe tocar la red: %s %s", r.Method, r.URL.Path)
				})

				args := append([]string{"transport-documents", tc.action, id}, tc.extra...)
				args = append(append(args, confirm.args...), "--skip-scope-check", "--no-input")
				_, _, err := runTasksCLI(t, args...)
				if err == nil || !strings.Contains(err.Error(), confirm.want) {
					t.Fatalf("esperaba que se negara pidiendo confirmación (%q), got %v", confirm.want, err)
				}
				if exit.ForError(err) != exit.Usage {
					t.Errorf("exit code = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
				}
			})
		}
	}
}

// TestTransportDocumentsTransitionsReachTheAPI — confirmadas con el id del
// documento, las irreversibles llegan a su ruta con su cuerpo y la clave de
// idempotencia que exigen; cerrar y reabrir no piden confirmación.
func TestTransportDocumentsTransitionsReachTheAPI(t *testing.T) {
	const id = "0199f3a2-525d-7000-8000-0000000000d1"

	cases := []struct {
		action string
		scope  string
		args   []string
		check  func(t *testing.T, body map[string]any)
	}{
		{action: "issue", scope: "transport_documents:transition", args: []string{"--confirm", id}},
		{action: "modify", scope: "transport_documents:transition", args: []string{"--confirm", id,
			"--reason", "Cambio de matrícula", "--data-origin", "carrier_reported", "--vehicle.tractor-plate", "5678FGH"},
			check: func(t *testing.T, body map[string]any) {
				vehicle, _ := body["vehicle"].(map[string]any)
				if body["reason"] != "Cambio de matrícula" || body["data_origin"] != "carrier_reported" || vehicle["tractor_plate"] != "5678FGH" {
					t.Errorf("cuerpo de modify mal formado: %v", body)
				}
			}},
		{action: "cancel", scope: "transport_documents:transition", args: []string{"--confirm", id, "--reason", "El cliente cancela el pedido"},
			check: func(t *testing.T, body map[string]any) {
				if body["reason"] != "El cliente cancela el pedido" {
					t.Errorf("cuerpo de cancel sin reason: %v", body)
				}
			}},
		{action: "supersede", scope: "transport_documents:transition", args: []string{"--confirm", id,
			"--reference", "DC-TRANSP-2026-0042", "--reason", "Lo sustituye el documento del transportista"},
			check: func(t *testing.T, body map[string]any) {
				if body["reference"] != "DC-TRANSP-2026-0042" || body["reason"] == nil {
					t.Errorf("cuerpo de supersede mal formado: %v", body)
				}
			}},
		{action: "send", scope: "transport_documents:write", args: []string{"--confirm", id, "--to", "transporte@cliente.example"},
			check: func(t *testing.T, body map[string]any) {
				if to := stringSliceOf(body["to"]); strings.Join(to, ",") != "transporte@cliente.example" {
					t.Errorf("cuerpo de send: to = %v, quiero [transporte@cliente.example]", body["to"])
				}
			}},
		{action: "close", scope: "transport_documents:transition"},
		{action: "reopen", scope: "transport_documents:transition", args: []string{"--reason", "Falta una entrega"}},
	}

	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			var method, path, idempotency string
			var body map[string]any
			tasksTestServer(t, `"`+tc.scope+`"`, func(w http.ResponseWriter, r *http.Request) {
				method, path = r.Method, r.URL.Path
				idempotency = r.Header.Get("Idempotency-Key")
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &body)
				_, _ = w.Write([]byte(`{"data":{"id":"` + id + `","object":"transport_document"}}`))
			})

			args := append([]string{"transport-documents", tc.action, id, "--json", "--no-input"}, tc.args...)
			if _, stderr, err := runTasksCLI(t, args...); err != nil {
				t.Fatalf("execute: %v (stderr: %s)", err, stderr)
			}
			if want := "/v1/companies/acme_co/transport-documents/" + id + "/" + tc.action; method != http.MethodPost || path != want {
				t.Fatalf("petición inesperada: %s %s, quiero POST %s", method, path, want)
			}
			if idempotency == "" {
				t.Error("la transición debe llevar Idempotency-Key")
			}
			if tc.check != nil {
				tc.check(t, body)
			}
		})
	}
}

// TestTransportDocumentsBinaryDownloadsWriteTheFileVerbatim — el PDF, el QR y el
// justificante se escriben byte a byte al fichero de `-o`, con sus parámetros de
// consulta. El cuerpo servido NO es JSON: si el CLI intentara parsearlo, fallaría.
func TestTransportDocumentsBinaryDownloadsWriteTheFileVerbatim(t *testing.T) {
	const id = "0199f3a2-525d-7000-8000-0000000000d1"
	pdf := []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n1 0 obj\x00\xff\n%%EOF\n")
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\xff\xfe")

	cases := []struct {
		name        string
		args        []string
		scope       string
		path        string
		query       string
		contentType string
		served      []byte
	}{
		{"pdf de una versión", []string{"pdf", id, "--version", "2", "--download", "1"}, "pdfs:read",
			"/v1/companies/acme_co/transport-documents/" + id + "/pdf", "download=1&version=2", "application/pdf", pdf},
		{"qr en png", []string{"qr", id, "--format", "png"}, "transport_documents:read",
			"/v1/companies/acme_co/transport-documents/" + id + "/qr", "format=png", "image/png", png},
		{"justificante de emisión", []string{"issuance-proof", id}, "transport_documents:read",
			"/v1/companies/acme_co/transport-documents/" + id + "/issuance-proof", "", "application/pdf", pdf},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var path, query string
			tasksTestServer(t, `"`+tc.scope+`"`, func(w http.ResponseWriter, r *http.Request) {
				path, query = r.URL.Path, r.URL.RawQuery
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = w.Write(tc.served)
			})

			out := filepath.Join(t.TempDir(), "descarga")
			args := append(append([]string{"transport-documents"}, tc.args...), "-o", out)
			stdout, stderr, err := runTasksCLI(t, args...)
			if err != nil {
				t.Fatalf("execute: %v (stderr: %s)", err, stderr)
			}
			if path != tc.path || query != tc.query {
				t.Errorf("petición = %s?%s, quiero %s?%s", path, query, tc.path, tc.query)
			}
			written, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("el fichero de -o no se escribió: %v", err)
			}
			if !bytes.Equal(written, tc.served) {
				t.Errorf("el fichero no es byte a byte lo servido: %q, quiero %q", written, tc.served)
			}
			if stdout != "" {
				t.Errorf("con -o la salida estándar debe ir vacía, y trae %q", stdout)
			}
		})
	}
}

// TestTransportDocumentsScopeCheckRefusesWithoutTheRequiredScope — la comprobación
// local de ámbitos usa el `x-required-scope` de cada operación: una key sin él se
// rechaza con código de permisos y la operación no llega a la API. El PDF pide
// `pdfs:read` y el justificante `transport_documents:read`, al revés de lo que
// sugeriría el nombre, como en el backend.
func TestTransportDocumentsScopeCheckRefusesWithoutTheRequiredScope(t *testing.T) {
	const id = "0199f3a2-525d-7000-8000-0000000000d1"

	cases := []struct {
		name      string
		keyScopes string
		args      []string
		missing   string
	}{
		{"pdf sin pdfs:read", `"transport_documents:read"`, []string{"pdf", id}, "pdfs:read"},
		{"justificante solo con pdfs:read", `"pdfs:read"`, []string{"issuance-proof", id}, "transport_documents:read"},
		{"emitir sin transition", `"transport_documents:read","transport_documents:write"`, []string{"issue", id, "--confirm", id}, "transport_documents:transition"},
		{"enviar sin write", `"transport_documents:transition"`, []string{"send", id, "--confirm", id}, "transport_documents:write"},
		{"borrar sin delete", `"transport_documents:write"`, []string{"delete", id}, "transport_documents:delete"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tasksTestServer(t, tc.keyScopes, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("sin el scope requerido la operación NO debe llegar a la API: %s %s", r.Method, r.URL.Path)
			})

			_, stderr, err := runTasksCLI(t, append([]string{"transport-documents"}, tc.args...)...)
			if err == nil {
				t.Fatal("esperaba que la comprobación de ámbitos rechazara la operación")
			}
			if exit.ForError(err) != exit.Perm {
				t.Errorf("exit code = %d, want %d (Perm)", exit.ForError(err), exit.Perm)
			}
			if !strings.Contains(stderr, tc.missing) {
				t.Errorf("el error debe nombrar el scope %q que falta: %s", tc.missing, stderr)
			}
		})
	}
}

// transportDocumentKeys son las claves de primer nivel del recurso de respuesta,
// medidas sobre el spec regenerado. Todas son `required`.
var transportDocumentKeys = []string{
	"cancel_reason", "cancelled_at", "carrier", "close_mode", "closed_at", "company_role",
	"created_at", "current_version", "files_purged_at", "id", "issued_at", "last_modified_at",
	"number", "object", "observations", "public_url", "reopen_deadline", "retention_until",
	"shipments", "shipper", "special_authorization", "status", "superseded_at",
	"superseding_reference", "transport_date", "updated_at", "vehicle", "versions",
}

var transportDocumentEnums = map[string][]string{
	"object":       {"transport_document"},
	"status":       {"draft", "issued", "closed", "cancelled", "superseded"},
	"company_role": {"shipper", "carrier"},
	"close_mode":   {"manual", "automatic"},
}

// TestTransportDocumentResponseSchemaIsFrozen — el contrato de RESPUESTA del DeCA,
// leído del spec embebido: el manifiesto no lo atraviesa.
func TestTransportDocumentResponseSchemaIsFrozen(t *testing.T) {
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				// RawMessage: una propiedad puede ser el esquema booleano `false`
				// (campo reservado), que no decodifica a un objeto.
				Properties map[string]json.RawMessage `json:"properties"`
				Required   []string                   `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(spec.Raw, &doc); err != nil {
		t.Fatalf("el spec embebido no es JSON: %v", err)
	}
	schema, ok := doc.Components.Schemas["TransportDocument"]
	if !ok {
		t.Fatal("el spec embebido no declara el schema TransportDocument: el recurso desapareció entero")
	}

	want := append([]string(nil), transportDocumentKeys...)
	sort.Strings(want)
	got := make([]string, 0, len(schema.Properties))
	for k := range schema.Properties {
		got = append(got, k)
	}
	sort.Strings(got)
	if diff := missingFrom(want, got); len(diff) > 0 {
		t.Errorf("TransportDocument.properties ya no declara %v: el contrato de respuesta RETROCEDIÓ", diff)
	}
	if diff := missingFrom(got, want); len(diff) > 0 {
		t.Errorf("TransportDocument.properties declara %v, que esta tabla no congela: la API amplió el contrato", diff)
	}

	required := append([]string(nil), schema.Required...)
	sort.Strings(required)
	if diff := missingFrom(want, required); len(diff) > 0 {
		t.Errorf("TransportDocument.required ya no incluye %v", diff)
	}

	for key, wantEnum := range transportDocumentEnums {
		raw, ok := schema.Properties[key]
		if !ok {
			continue // su ausencia ya la reporta la comparación de conjuntos
		}
		var prop struct {
			Enum []any `json:"enum"`
		}
		if err := json.Unmarshal(raw, &prop); err != nil {
			t.Fatalf("TransportDocument.%s no es un esquema de objeto: %v", key, err)
		}
		if gotEnum := stringEnumOf(prop.Enum); strings.Join(gotEnum, ",") != strings.Join(wantEnum, ",") {
			t.Errorf("TransportDocument.%s.enum = %v, quiero %v", key, gotEnum, wantEnum)
		}
	}
}
