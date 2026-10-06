package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
	"github.com/spf13/cobra"
)

func runCmd(t *testing.T, baseURL string, args ...string) (string, error) {
	t.Helper()
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	if baseURL != "" {
		t.Setenv("FACTUAREA_BASE_URL", baseURL)
	}
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestTypedFlagsBuildBodyWithTypes(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"cnt_1"}}`))
	}))
	t.Cleanup(srv.Close)

	_, err := runCmd(t, srv.URL, "contacts", "create", "--skip-scope-check",
		"--name", "ACME SL", "--kind", "company", "--roles", "customer", "--tax-id", "B12345678",
		"--customer-profile.payment-terms-days", "30", "--address.city", "Madrid",
		"--billing-emails", "a@x.com,b@x.com", "--metadata", "erp=CLI-1", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got["name"] != "ACME SL" || got["tax_id"] != "B12345678" {
		t.Errorf("strings mal mapeados: %v", got)
	}
	roles, ok := got["roles"].([]any)
	if !ok || len(roles) != 1 || roles[0] != "customer" {
		t.Errorf("roles debe ser slice con customer: %v", got["roles"])
	}
	profile, ok := got["customer_profile"].(map[string]any)
	if !ok {
		t.Fatalf("customer_profile debe agruparse en objeto: %v", got["customer_profile"])
	}
	if n, ok := profile["payment_terms_days"].(float64); !ok || n != 30 {
		t.Errorf("customer_profile.payment_terms_days debe ser número 30: %v", profile["payment_terms_days"])
	}
	addr, ok := got["address"].(map[string]any)
	if !ok || addr["city"] != "Madrid" {
		t.Errorf("address.city debe agruparse en objeto: %v", got["address"])
	}
	emails, ok := got["billing_emails"].([]any)
	if !ok || len(emails) != 2 {
		t.Errorf("billing_emails debe ser slice de 2: %v", got["billing_emails"])
	}
	meta, ok := got["metadata"].(map[string]any)
	if !ok || meta["erp"] != "CLI-1" {
		t.Errorf("metadata debe ser map: %v", got["metadata"])
	}
}

func TestTypedFlagsZeroVsOmitted(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"cnt_1"}}`))
	}))
	t.Cleanup(srv.Close)

	_, err := runCmd(t, srv.URL, "contacts", "create", "--skip-scope-check",
		"--name", "X", "--kind", "person", "--roles", "customer", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, present := got["customer_profile"]; present {
		t.Errorf("customer_profile omitido no debe estar presente: %v", got)
	}

	got = nil
	_, err = runCmd(t, srv.URL, "contacts", "create", "--skip-scope-check",
		"--name", "X", "--kind", "person", "--roles", "customer", "--customer-profile.discount", "0", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	profile, ok := got["customer_profile"].(map[string]any)
	if !ok {
		t.Fatalf("customer_profile debe enviarse al usar un flag anidado: %v", got)
	}
	if v, ok := profile["discount"].(float64); !ok || v != 0 {
		t.Errorf("--customer-profile.discount 0 debe enviarse como 0: %v", profile["discount"])
	}
}

func TestMixingFlagsAndRawDataRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("la red NO debe tocarse al mezclar flags con -d")
	}))
	t.Cleanup(srv.Close)

	_, err := runCmd(t, srv.URL, "contacts", "create", "--name", "X", "-d", `{"name":"Y"}`)
	if err == nil || !strings.Contains(err.Error(), "no mezcles") {
		t.Fatalf("esperaba error de uso por mezcla, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit code = %d, want Usage", exit.ForError(err))
	}
}

func TestDataFromStdin(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"cnt_1"}}`))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(`{"name":"PIPED"}`))
	root.SetArgs([]string{"contacts", "create", "--skip-scope-check", "-d", "-", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got["name"] != "PIPED" {
		t.Errorf("stdin no llegó al body: %v", got)
	}
}

func TestDryRunPrintsBodyNoNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("la red NO debe tocarse en --dry-run")
	}))
	t.Cleanup(srv.Close)

	out, err := runCmd(t, srv.URL, "contacts", "create", "--name", "ACME", "--kind", "person", "--roles", "customer", "--dry-run")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var body map[string]any
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(out)), &body); jerr != nil {
		t.Fatalf("dry-run debe imprimir JSON válido: %q (%v)", out, jerr)
	}
	if body["name"] != "ACME" {
		t.Errorf("dry-run body mal: %v", body)
	}
}

func TestSkeletonEmitsTemplateNoNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("la red NO debe tocarse en --skeleton")
	}))
	t.Cleanup(srv.Close)

	out, err := runCmd(t, srv.URL, "contacts", "create", "--skeleton")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var body map[string]any
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(out)), &body); jerr != nil {
		t.Fatalf("skeleton debe ser JSON válido: %q (%v)", out, jerr)
	}
	if _, ok := body["name"]; !ok {
		t.Errorf("skeleton debe incluir el campo required name: %v", body)
	}
	if kind, ok := body["kind"].(string); !ok || !strings.Contains(kind, "company") {
		t.Errorf("skeleton debe incluir hints de enum: %v", body["kind"])
	}
	profile, ok := body["customer_profile"].(map[string]any)
	if !ok {
		t.Fatalf("skeleton debe anidar customer_profile: %v", body["customer_profile"])
	}
	if pm, ok := profile["payment_method"].(string); !ok || !strings.Contains(pm, "direct_debit") {
		t.Errorf("skeleton debe incluir hints de enum anidados: %v", profile["payment_method"])
	}
	if addr, ok := body["address"].(map[string]any); !ok || addr["city"] == nil {
		t.Errorf("skeleton debe anidar objetos prof.1: %v", body["address"])
	}
}

func TestObjectArrayOpHasNoLineFlagsAndDirectsToFile(t *testing.T) {
	_, err := runCmd(t, "", "invoices", "create", "--lines", "x", "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "flag desconocido") {
		t.Fatalf("invoices create NO debe registrar --lines, got %v", err)
	}

	out, herr := runCmd(t, "", "invoices", "create", "--help")
	if herr != nil {
		t.Fatalf("help: %v", herr)
	}
	if !strings.Contains(out, "--data-file") || !strings.Contains(out, "lista de objetos") {
		t.Errorf("help debe dirigir a --data-file para arrays de objetos: %s", out)
	}
}

func TestUpdateHelpDescribesPartialEdit(t *testing.T) {
	out, err := runCmd(t, "", "contacts", "update", "--help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(out, "Edición parcial") || !strings.Contains(out, "los omitidos se conservan") {
		t.Errorf("update help debe describir la edición parcial: %s", out)
	}
}

func TestManifestIncludesFieldSchema(t *testing.T) {
	out, err := runCmd(t, "", "commands", "--json")
	if err != nil {
		t.Fatalf("commands: %v", err)
	}
	var manifest []map[string]any
	if jerr := json.Unmarshal([]byte(out), &manifest); jerr != nil {
		t.Fatalf("manifest no es JSON: %v", jerr)
	}
	var create map[string]any
	for _, e := range manifest {
		if e["command"] == "factuarea contacts create" {
			create = e
			break
		}
	}
	if create == nil {
		t.Fatal("manifest sin contacts create")
	}
	fields, ok := create["body_fields"].([]any)
	if !ok || len(fields) == 0 {
		t.Fatalf("contacts create debe traer body_fields: %v", create["body_fields"])
	}
	var sawName, sawEnum bool
	for _, raw := range fields {
		f := raw.(map[string]any)
		if f["name"] == "name" && f["required"] == true {
			sawName = true
		}
		if f["name"] == "customer-profile.payment-method" {
			if enum, ok := f["enum"].([]any); ok && len(enum) > 0 {
				sawEnum = true
			}
		}
	}
	if !sawName {
		t.Error("manifest debe marcar name como required")
	}
	if !sawEnum {
		t.Error("manifest debe incluir enum de customer_profile.payment_method")
	}
}

// nestedBodyOp es una operación sintética con un objeto `link` cuyos hijos `type`
// e `id` son requeridos. `objectRequired` decide si el propio objeto lo es.
func nestedBodyOp(objectRequired bool) genOp {
	return genOp{
		Method: "POST",
		Body: &genBody{Kind: "json", Fields: []genBodyField{
			{Name: "title", Type: "string", Kind: "scalar", Required: true},
			{Name: "link", Type: "object", Kind: "object", Required: objectRequired, Children: []genBodyField{
				{Name: "type", Type: "string", Kind: "scalar", Required: true},
				{Name: "id", Type: "string", Kind: "scalar", Required: true},
				{Name: "note", Type: "string", Kind: "scalar"},
			}},
		}},
	}
}

func requiredFlagNames(op genOp) []string {
	var names []string
	for _, ff := range requiredBodyFlags(op) {
		names = append(names, ff.flagName)
	}
	return names
}

func TestRequiredBodyFlagsSkipRequiredChildrenOfAnOptionalObject(t *testing.T) {
	got := requiredFlagNames(nestedBodyOp(false))
	if strings.Join(got, ",") != "title" {
		t.Fatalf("un hijo requerido de un objeto opcional no es requerido por sí solo: got %v, want [title]", got)
	}
}

func TestRequiredBodyFlagsKeepRequiredChildrenOfARequiredObject(t *testing.T) {
	got := requiredFlagNames(nestedBodyOp(true))
	if strings.Join(got, ",") != "title,link.type,link.id" {
		t.Fatalf("un hijo requerido de un objeto requerido sí lo es: got %v, want [title link.type link.id]", got)
	}
}

func TestFieldHelpMarksRequiredOnlyWhenEveryAncestorIsRequired(t *testing.T) {
	cases := []struct {
		name           string
		objectRequired bool
		wantRequired   map[string]bool
	}{
		{"objeto opcional", false, map[string]bool{"--title": true, "--link.type": false, "--link.id": false, "--link.note": false}},
		{"objeto requerido", true, map[string]bool{"--title": true, "--link.type": true, "--link.id": true, "--link.note": false}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := nestedBodyOp(tc.objectRequired)

			lines := map[string]string{}
			for _, line := range strings.Split(bodyFieldsHelp(op), "\n") {
				if fields := strings.Fields(line); len(fields) > 0 && strings.HasPrefix(fields[0], "--") {
					lines[fields[0]] = line
				}
			}
			for flag, want := range tc.wantRequired {
				line, ok := lines[flag]
				if !ok {
					t.Errorf("la ayuda no lista %s: %q", flag, bodyFieldsHelp(op))
					continue
				}
				if got := strings.Contains(line, "requerido"); got != want {
					t.Errorf("ayuda de %s: requerido = %v, quiero %v (%q)", flag, got, want, line)
				}
			}

			for _, ff := range collectFieldFlags(op.Body.Fields, nil) {
				desc := fieldFlagDescription(op, ff)
				if got := strings.Contains(desc, "(requerido)"); got != tc.wantRequired["--"+ff.flagName] {
					t.Errorf("descripción del flag --%s: %q", ff.flagName, desc)
				}
			}
		})
	}
}

func TestOptionalObjectIsAllOrNothing(t *testing.T) {
	cases := []struct {
		name           string
		objectRequired bool
		args           []string
		wantMissing    []string
	}{
		{"objeto opcional omitido", false, []string{"--title", "x"}, nil},
		{"objeto opcional completo", false, []string{"--title", "x", "--link.type", "quote", "--link.id", "q1"}, nil},
		{"objeto opcional sin id", false, []string{"--title", "x", "--link.type", "quote"}, []string{"--link.id"}},
		{"objeto opcional sin type", false, []string{"--title", "x", "--link.id", "q1"}, []string{"--link.type"}},
		{"objeto opcional con solo un hijo opcional", false, []string{"--title", "x", "--link.note", "n"}, []string{"--link.type", "--link.id"}},
		{"objeto requerido omitido", true, []string{"--title", "x"}, []string{"--link.type", "--link.id"}},
		{"objeto requerido completo", true, []string{"--title", "x", "--link.type", "quote", "--link.id", "q1"}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := nestedBodyOp(tc.objectRequired)
			cmd := &cobra.Command{Use: "x"}
			registerFieldFlags(cmd, op)
			if err := cmd.ParseFlags(tc.args); err != nil {
				t.Fatalf("flags: %v", err)
			}

			err := validateRequiredBodyFlags(cmd, op)
			if len(tc.wantMissing) == 0 {
				if err != nil {
					t.Fatalf("no debería faltar nada, y falla con: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("debería faltar %v y no falla", tc.wantMissing)
			}
			if exit.ForError(err) != exit.Usage {
				t.Errorf("exit code = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
			}
			want := "faltan campos requeridos: " + strings.Join(tc.wantMissing, ", ") + " ("
			if !strings.Contains(err.Error(), want) {
				t.Errorf("mensaje = %q, quiero que contenga %q", err.Error(), want)
			}
		})
	}
}

// TestManifestRequiredFollowsTheAncestorRule ata el manifiesto de `commands --json`
// al predicado que aplican la validación y la ayuda: para TODA operación con
// cuerpo tipado, un campo se anuncia como requerido si y solo si fieldRequired.
func TestManifestRequiredFollowsTheAncestorRule(t *testing.T) {
	checked := 0
	for _, op := range generatedOps() {
		if !op.typedBody() {
			continue
		}
		manifest := map[string]bool{}
		for _, f := range manifestFields(op.Body.Fields, nil, true) {
			manifest[f.Name] = f.Required
		}
		for _, ff := range collectFieldFlags(op.Body.Fields, nil) {
			got, ok := manifest[ff.flagName]
			if !ok {
				t.Errorf("%s: el manifiesto no trae el campo %s", commandPath(op), ff.flagName)
				continue
			}
			checked++
			if want := fieldRequired(op.Body.Fields, ff.jsonPath); got != want {
				t.Errorf("%s: %s anunciado required=%v, la regla de ancestros da %v", commandPath(op), ff.flagName, got, want)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no se comprobó ningún campo: el árbol generado no tiene cuerpos tipados")
	}
}

func TestManifestRequiredSkipsRequiredChildrenOfAnOptionalObject(t *testing.T) {
	cases := []struct {
		name           string
		objectRequired bool
		want           map[string]bool
	}{
		{"objeto opcional", false, map[string]bool{"title": true, "link.type": false, "link.id": false, "link.note": false}},
		{"objeto requerido", true, map[string]bool{"title": true, "link.type": true, "link.id": true, "link.note": false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := map[string]bool{}
			for _, f := range manifestFields(nestedBodyOp(tc.objectRequired).Body.Fields, nil, true) {
				got[f.Name] = f.Required
			}
			for name, want := range tc.want {
				if got[name] != want {
					t.Errorf("manifiesto: %s required = %v, quiero %v", name, got[name], want)
				}
			}
		})
	}
}

// TestSinglePositionalFieldIgnoresRequiredChildrenOfAnOptionalObject — el único
// campo escalar de una operación solo es su argumento posicional si de verdad es
// requerido.
func TestSinglePositionalFieldIgnoresRequiredChildrenOfAnOptionalObject(t *testing.T) {
	build := func(objectRequired bool) genOp {
		return genOp{
			Method: "POST",
			Body: &genBody{Kind: "json", Fields: []genBodyField{
				{Name: "link", Type: "object", Kind: "object", Required: objectRequired, Children: []genBodyField{
					{Name: "id", Type: "string", Kind: "scalar", Required: true},
				}},
			}},
		}
	}

	if ff, ok := singlePositionalField(build(false)); ok {
		t.Errorf("el hijo requerido de un objeto opcional no es un argumento posicional: %s", ff.flagName)
	}
	if ff, ok := singlePositionalField(build(true)); !ok || ff.flagName != "link.id" {
		t.Errorf("el hijo requerido de un objeto requerido sí lo es: got %q/%v", ff.flagName, ok)
	}
}

// TestUnionTypedFlagSendsTextAndPointsToTheRawBody — un campo de tipo unión se
// presenta con su tipo completo, su flag envía texto y la ayuda dice que lo
// tipado, las listas y null van por -d.
func TestUnionTypedFlagSendsTextAndPointsToTheRawBody(t *testing.T) {
	const union = "string|number|boolean|array|null"
	op := genOp{
		Method: "PUT",
		Body: &genBody{Kind: "json", Fields: []genBodyField{
			{Name: "value", Type: union, Kind: "scalar", Required: true, Nullable: true},
			{Name: "label", Type: "string", Kind: "scalar"},
		}},
	}

	if f := findField(op.Body.Fields, []string{"value"}); f == nil || !f.isUnionType() {
		t.Fatalf("value debe ser un campo de tipo unión: %+v", f)
	}
	if f := findField(op.Body.Fields, []string{"label"}); f == nil || f.isUnionType() {
		t.Fatalf("label no es una unión: %+v", f)
	}

	help := bodyFieldsHelp(op)
	for _, want := range []string{"--value (" + union + ") requerido", "Los flags de tipo unión (--value) envían texto", "-d/--data-file"} {
		if !strings.Contains(help, want) {
			t.Errorf("la ayuda debe contener %q:\n%s", want, help)
		}
	}
	if strings.Contains(help, "--label (string) requerido") || strings.Contains(help, "(--value, --label)") {
		t.Errorf("solo el campo unión lleva la nota:\n%s", help)
	}

	cmd := &cobra.Command{Use: "x"}
	registerFieldFlags(cmd, op)
	flag := cmd.Flags().Lookup("value")
	if flag == nil || flag.Value.Type() != "string" {
		t.Fatalf("el flag de una unión debe ser de texto: %+v", flag)
	}
	if !strings.Contains(flag.Usage, unionFlagHint) {
		t.Errorf("la descripción del flag debe llevar la pista de -d: %q", flag.Usage)
	}
	if err := cmd.ParseFlags([]string{"--value", "42"}); err != nil {
		t.Fatalf("flags: %v", err)
	}
	body, err := bodyFromFieldFlags(cmd, op)
	if err != nil {
		t.Fatalf("cuerpo: %v", err)
	}
	if body["value"] != "42" {
		t.Errorf("el flag envía texto: got %#v, quiero \"42\"", body["value"])
	}
}

func TestSeriesUpdateSendsAPartialPut(t *testing.T) {
	var got map[string]any
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"ser_1","counter_reset":"monthly"}}`))
	}))
	t.Cleanup(srv.Close)

	_, err := runCmd(t, srv.URL, "series", "update", "ser_1", "--skip-scope-check",
		"--name", "Tickets 2026", "--counter-reset", "monthly", "--initial-number", "10", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if method != http.MethodPut || !strings.HasSuffix(path, "/series/ser_1") {
		t.Errorf("debe ser PUT /series/ser_1: %s %s", method, path)
	}
	if got["name"] != "Tickets 2026" || got["counter_reset"] != "monthly" {
		t.Errorf("strings mal mapeados: %v", got)
	}
	if n, ok := got["initial_number"].(float64); !ok || n != 10 {
		t.Errorf("initial_number debe ser el número 10: %v", got["initial_number"])
	}
	if _, present := got["code"]; present {
		t.Errorf("un flag omitido no debe viajar en el cuerpo: %v", got)
	}
}
