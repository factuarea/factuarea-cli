package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

func TestGeneratedListCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == identityPath() {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
			return
		}
		if r.URL.Path != "/v1/companies/acme_co/invoices" {
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
	root.SetArgs([]string{"invoices", "list", "--company", "acme_co", "--json"})
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
	root.SetArgs([]string{"invoices", "create", "--company", "acme_co", "-d", "{}"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "LIVE") {
		t.Fatalf("esperaba guard LIVE, got %v", err)
	}
}

func TestGeneratedIrreversibleRequiresConfirmInNoInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("la red NO debe tocarse: recibí %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"invoices", "delete", "--company", "acme_co", "inv_123", "--skip-scope-check", "--no-input"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("esperaba fallo pidiendo --confirm, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit code = %d, want %d (Usage)", exit.ForError(err), exit.Usage)
	}
}

func TestGeneratedIrreversibleConfirmFlagReachesNetwork(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" && r.URL.Path == "/v1/companies/acme_co/invoices/inv_123" {
			hit = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"inv_123","deleted":true}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"invoices", "delete", "--company", "acme_co", "inv_123", "--skip-scope-check", "--confirm", "inv_123", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !hit {
		t.Fatal("la red debió recibir el DELETE tras --confirm correcto")
	}
}

func TestGeneratedMissingScopeBlocksWithExit4(t *testing.T) {
	var deleteHit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == identityPath():
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["clients:read"]}}}`))
		case r.URL.Path == "/v1/companies/acme_co/invoices/inv_123":
			deleteHit = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("petición inesperada: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"invoices", "delete", "--company", "acme_co", "inv_123", "--confirm", "inv_123", "--json"})
	err := root.Execute()
	if err == nil {
		t.Fatal("esperaba bloqueo por scope insuficiente")
	}
	if exit.ForError(err) != exit.Perm {
		t.Fatalf("exit code = %d, want %d (Perm)", exit.ForError(err), exit.Perm)
	}
	if deleteHit {
		t.Fatal("el endpoint de la operación NO debió recibir la petición (bloqueado pre-red)")
	}
}

func TestGeneratedSkipScopeCheckBypassesBlock(t *testing.T) {
	var deleteHit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == identityPath() {
			t.Fatalf("con --skip-scope-check no debe llamarse a %s", identityPath())
		}
		if r.URL.Path == "/v1/companies/acme_co/invoices/inv_123" {
			deleteHit = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"inv_123","deleted":true}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"invoices", "delete", "--company", "acme_co", "inv_123", "--confirm", "inv_123", "--skip-scope-check", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !deleteHit {
		t.Fatal("la red debió recibir el DELETE con --skip-scope-check")
	}
}

// ---------------------------------------------------------------------------
// Eje de empresa sobre el DOCUMENTO CONGELADO (395 paths · 480 operaciones)
// ---------------------------------------------------------------------------
// El spec EMBEBIDO de este binario sigue siendo el del contrato anterior.
const (
	frozenCompanyID = "01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8a01"
	frozenInvoiceID = "01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8b10"
	frozenAccountID = "01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8c20"
)

// frozenInvoicesShow: `GET /companies/{company}/invoices/{invoice}`, operación
// de eje con DOS parámetros de ruta.
func frozenInvoicesShow() genOp {
	return genOp{
		OperationID: "public-api.v1.invoices.show", Method: "GET",
		Path:   "/companies/{company}/invoices/{invoice}",
		Action: "show", Summary: "Retrieve an invoice",
		RequiredScope: "invoices:read",
		Groups:        []string{"invoices"},
		PathParams: []genParam{
			{Name: "company", In: "path", Type: "string", Required: true},
			{Name: "invoice", In: "path", Type: "string", Required: true},
		},
		QueryParams: []genParam{},
	}
}

// frozenInvoicesDelete: `DELETE /companies/{company}/invoices/{invoice}`.
func frozenInvoicesDelete() genOp {
	op := frozenInvoicesShow()
	op.OperationID = "public-api.v1.invoices.delete"
	op.Method = "DELETE"
	op.Action = "delete"
	op.Summary = "Delete an invoice"
	op.Irreversible = true
	op.RequiredScope = "invoices:delete"
	return op
}

// frozenManagedCompanyDelete: `DELETE /accounts/{account}/companies/{company}`,
// donde `{company}` NO es el eje sino el recurso archivado.
func frozenManagedCompanyDelete() genOp {
	return genOp{
		OperationID: "public-api.v1.companies.delete", Method: "DELETE",
		Path:   "/accounts/{account}/companies/{company}",
		Action: "delete", Summary: "Archive a managed company",
		RequiredScope: "companies:delete",
		Groups:        []string{"companies"},
		PathParams: []genParam{
			{Name: "account", In: "path", Type: "string", Required: true},
			{Name: "company", In: "path", Type: "string", Required: true},
		},
		QueryParams: []genParam{},
	}
}

// frozenInvoicesBulkDelete: `POST /companies/{company}/invoices/bulk-delete`.
func frozenInvoicesBulkDelete() genOp {
	return genOp{
		OperationID: "public-api.v1.invoices.bulk_delete", Method: "POST",
		Path:   "/companies/{company}/invoices/bulk-delete",
		Action: "bulk-delete", Summary: "Bulk delete invoices",
		Irreversible: true, RequiredScope: "invoices:delete",
		Groups: []string{"invoices"},
		PathParams: []genParam{
			{Name: "company", In: "path", Type: "string", Required: true},
		},
		QueryParams: []genParam{},
	}
}

// runFrozenOp ejecuta el comando generado de una operación fixture.
func runFrozenOp(t *testing.T, baseURL string, op genOp, g *GlobalFlags, args ...string) (string, error) {
	t.Helper()
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	if baseURL != "" {
		t.Setenv("FACTUAREA_BASE_URL", baseURL)
	}
	t.Cleanup(resetCompanyResolution)

	c := buildGeneratedCommand(op)
	c.SilenceUsage = true
	c.SilenceErrors = true
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetContext(context.WithValue(context.Background(), globalsKey{}, g))
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

// silentServer falla el test ante CUALQUIER petición.
func silentServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("la red NO debía tocarse: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestFrozenAxisOperationWithTwoPathParams es el golden del eje.
func TestFrozenAxisOperationWithTwoPathParams(t *testing.T) {
	op := frozenInvoicesShow()
	c := buildGeneratedCommand(op)

	if want := "show [company] <invoice>"; c.Use != want {
		t.Fatalf("Use = %q, want %q", c.Use, want)
	}
	if !strings.Contains(c.Long, "--company") {
		t.Errorf("la ayuda larga debe explicar la flag persistente --company; got:\n%s", c.Long)
	}
	if !strings.Contains(c.Long, "Forma corta:") {
		t.Errorf("la ayuda larga debe documentar la forma corta; got:\n%s", c.Long)
	}

	for _, tc := range []struct {
		args []string
		ok   bool
		why  string
	}{
		{[]string{frozenInvoiceID}, true, "sólo el recurso: la empresa la aporta la cadena de precedencia"},
		{[]string{frozenCompanyID, frozenInvoiceID}, true, "empresa y recurso como posicionales"},
		{nil, false, "sin argumentos no hay factura que mostrar"},
		{[]string{frozenCompanyID, frozenInvoiceID, "sobra"}, false, "un posicional de más"},
	} {
		err := c.Args(c, tc.args)
		if tc.ok && err != nil {
			t.Errorf("args %v deberían aceptarse (%s): %v", tc.args, tc.why, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("args %v deberían rechazarse (%s)", tc.args, tc.why)
		}
	}

	const wantPath = "/v1/companies/" + frozenCompanyID + "/invoices/" + frozenInvoiceID
	for _, tc := range []struct {
		name    string
		args    []string
		company string
	}{
		{"los dos posicionales", []string{frozenCompanyID, frozenInvoiceID}, ""},
		{"empresa resuelta", []string{frozenInvoiceID}, frozenCompanyID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pathValues, extra, err := op.splitPositionals(tc.args, tc.company)
			if err != nil {
				t.Fatalf("reparto de posicionales: %v", err)
			}
			if len(extra) != 0 {
				t.Fatalf("no debería sobrar ningún posicional: %v", extra)
			}
			if len(pathValues) != 2 || pathValues[0] != frozenCompanyID || pathValues[1] != frozenInvoiceID {
				t.Fatalf("lista normalizada = %v, want [%s %s]", pathValues, frozenCompanyID, frozenInvoiceID)
			}
			if got := op.buildPath(pathValues); got != wantPath {
				t.Fatalf("path = %q, want %q", got, wantPath)
			}
		})
	}
}

// TestFrozenAxisDeleteConfirmationNamesTheResource.
func TestFrozenAxisDeleteConfirmationNamesTheResource(t *testing.T) {
	srv := silentServer(t)
	g := &GlobalFlags{Company: frozenCompanyID, NoInput: true}

	_, err := runFrozenOp(t, srv.URL, frozenInvoicesDelete(), g, frozenInvoiceID, "--skip-scope-check")
	if err == nil {
		t.Fatal("una operación irreversible sin --confirm debe fallar")
	}
	if !strings.Contains(err.Error(), "--confirm="+frozenInvoiceID) {
		t.Fatalf("la confirmación debe nombrar el RECURSO (%s): %v", frozenInvoiceID, err)
	}
	if strings.Contains(err.Error(), frozenCompanyID) {
		t.Fatalf("la confirmación NO debe nombrar la empresa: %v", err)
	}
}

// TestFrozenManagedCompanyIsResourceNotAxis fija la corrección del predicado.
func TestFrozenManagedCompanyIsResourceNotAxis(t *testing.T) {
	op := frozenManagedCompanyDelete()

	if idx := op.companyPathParamIndex(); idx != -1 {
		t.Fatalf("companyPathParamIndex = %d, want -1: aquí {company} es el recurso, no el eje", idx)
	}
	c := buildGeneratedCommand(op)
	if want := "delete <account> <company>"; c.Use != want {
		t.Fatalf("Use = %q, want %q: los dos parámetros son obligatorios", c.Use, want)
	}
	if strings.Contains(c.Long, "--company") {
		t.Errorf("la ayuda no debe ofrecer --company en una operación del eje de cuenta:\n%s", c.Long)
	}
	if err := c.Args(c, []string{frozenAccountID}); err == nil {
		t.Error("con un solo posicional debe fallar: aquí no hay relleno por defecto")
	}
	if err := c.Args(c, []string{frozenAccountID, frozenCompanyID}); err != nil {
		t.Errorf("con los dos posicionales debe aceptarse: %v", err)
	}

	if _, _, err := op.splitPositionals([]string{frozenCompanyID}, frozenCompanyID); err == nil {
		t.Error("la flag persistente NO debe rellenar el destino de un archivado de empresa gestionada")
	}

	pathValues := []string{frozenAccountID, frozenCompanyID}
	if got := opResourceID(op, pathValues); got != frozenCompanyID {
		t.Fatalf("el recurso confirmado = %q, want %q (la EMPRESA archivada, no la cuenta)", got, frozenCompanyID)
	}
	if want := "/v1/accounts/" + frozenAccountID + "/companies/" + frozenCompanyID; op.buildPath(pathValues) != want {
		t.Fatalf("path = %q, want %q", op.buildPath(pathValues), want)
	}
}

// TestFrozenIrreversibleAxisOperationDemandsExplicitCompany es la salvaguarda:
// cuando el eje ES el recurso y la operación es irreversible, el valor se
// escribe.
func TestFrozenIrreversibleAxisOperationDemandsExplicitCompany(t *testing.T) {
	op := frozenInvoicesBulkDelete()

	if !op.requiresExplicitCompany() {
		t.Fatal("una irreversible cuyo último path param es el eje debe exigir el valor explícito")
	}
	if idx := op.companyPathParamIndex(); idx != 0 {
		t.Fatalf("companyPathParamIndex = %d, want 0: sigue siendo el eje", idx)
	}
	if idx := op.companyFillIndex(); idx != -1 {
		t.Fatalf("companyFillIndex = %d, want -1: no admite relleno por defecto", idx)
	}

	c := buildGeneratedCommand(op)
	if want := "bulk-delete <company>"; c.Use != want {
		t.Fatalf("Use = %q, want %q: el eje se muestra obligatorio", c.Use, want)
	}
	if strings.Contains(c.Long, "Forma corta:") {
		t.Errorf("no hay forma corta que documentar donde no hay relleno por defecto:\n%s", c.Long)
	}

	srv := silentServer(t)
	g := &GlobalFlags{Company: frozenCompanyID, NoInput: true}
	_, err := runFrozenOp(t, srv.URL, op, g, "--skip-scope-check")
	if err == nil {
		t.Fatal("sin el posicional debe fallar en vez de rellenarse con --company")
	}
	if !strings.Contains(err.Error(), "irreversible") || !strings.Contains(err.Error(), "--company") {
		t.Fatalf("el mensaje debe decir por qué no se rellena: %v", err)
	}

	// Escrito a mano sí vale, y entonces lo que se confirma es la empresa.
	_, err = runFrozenOp(t, srv.URL, op, g, frozenCompanyID, "--skip-scope-check")
	if err == nil || !strings.Contains(err.Error(), "--confirm="+frozenCompanyID) {
		t.Fatalf("con el eje escrito, la confirmación debe nombrar la empresa: %v", err)
	}
}

// frozenManagedCompanyArchive: `DELETE /companies/{company}` tal y como la
// declara el contrato embebido hoy, SIN la marca de irreversible.
func frozenManagedCompanyArchive() genOp {
	return genOp{
		OperationID: "public-api.v1.companies.delete", Method: "DELETE",
		Path:   "/companies/{company}",
		Action: "delete", Summary: "Archive a managed company",
		Irreversible: false, RequiredScope: "companies:delete",
		Groups: []string{"companies"},
		PathParams: []genParam{
			{Name: "company", In: "path", Type: "string", Required: true},
		},
		QueryParams: []genParam{},
	}
}

// frozenManagedCompanyShow: `GET /companies/{company}`, misma forma y mismo
// grupo que la anterior, pero consulta.
func frozenManagedCompanyShow() genOp {
	op := frozenManagedCompanyArchive()
	op.OperationID = "public-api.v1.companies.show"
	op.Method = "GET"
	op.Action = "show"
	op.Summary = "Retrieve a managed company"
	op.RequiredScope = "companies:read"
	return op
}

// TestFrozenWholeCompanyMutationRejectsTheAutomaticAxis cierra el hueco de
// condicionar la salvaguarda a `x-irreversible`: sin él, una invocación sin un
// solo argumento archivaba la empresa de la credencial, y sin confirmación.
func TestFrozenWholeCompanyMutationRejectsTheAutomaticAxis(t *testing.T) {
	op := frozenManagedCompanyArchive()

	if op.Irreversible {
		t.Fatal("el fixture debe reproducir el contrato: esta operación NO lleva la marca")
	}
	if !op.rejectsAutomaticCompany() {
		t.Fatal("una mutación cuyo recurso ES la empresa no admite el eje deducido")
	}

	srv := silentServer(t)
	out, err := runFrozenOp(t, srv.URL, op, &GlobalFlags{NoInput: true}, "--skip-scope-check")
	if err == nil {
		t.Fatalf("sin empresa por ninguna vía debe fallar en vez de archivar la de la credencial; salida: %s", out)
	}
	if !strings.Contains(err.Error(), "empresa ENTERA") || !strings.Contains(err.Error(), "--company") {
		t.Fatalf("el fallo debe nombrar las dos vías escritas: %v", err)
	}

	// Escrita, sí: las dos vías valen, porque las dos las teclea el usuario.
	if _, _, serr := op.splitPositionals([]string{frozenCompanyID}, ""); serr != nil {
		t.Fatalf("el posicional debe seguir valiendo: %v", serr)
	}
	if _, err := op.resolveCompanyForArgs(context.Background(), &GlobalFlags{Company: frozenCompanyID}, nil); err != nil {
		t.Fatalf("--company debe seguir valiendo: %v", err)
	}
}

// TestFrozenWholeCompanyQueryStillResolvesTheAxis es la otra mitad.
func TestFrozenWholeCompanyQueryStillResolvesTheAxis(t *testing.T) {
	show := frozenManagedCompanyShow()
	if show.rejectsAutomaticCompany() {
		t.Fatal("una consulta no puede rechazar el eje deducido")
	}
	if idx := show.companyFillIndex(); idx != 0 {
		t.Fatalf("companyFillIndex = %d, want 0", idx)
	}

	create := frozenInvoicesCreate()
	if create.rejectsAutomaticCompany() {
		t.Fatal("crear una factura no rechaza el eje deducido: su recurso es la factura, no la empresa")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != identityPath() {
			t.Errorf("solo debía leerse el ámbito: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"scope":[{"id":"` + frozenCompanyID + `","tax_id":"B12345678","name":"Acme"}]}}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	t.Cleanup(resetCompanyResolution)

	got, err := show.resolveCompanyForArgs(context.Background(), &GlobalFlags{}, nil)
	if err != nil {
		t.Fatalf("la consulta debe deducir el eje de un ámbito de un solo NIF: %v", err)
	}
	if got != frozenCompanyID {
		t.Fatalf("empresa deducida = %q, want %q", got, frozenCompanyID)
	}
}

// TestSinglePositionalFieldStillRequiresZeroPathParams es la NO REGRESIÓN del
// predicado que deja VACÍO el universo de la aridad ambigua.
func TestSinglePositionalFieldStillRequiresZeroPathParams(t *testing.T) {
	body := &genBody{Kind: "json", Fields: []genBodyField{
		{Name: "sku", Type: "string", Kind: "scalar", Required: true},
	}}
	withoutPath := genOp{
		OperationID: "public-api.v1.products.find_by_sku", Method: "POST",
		Path: "/products/find-by-sku", Action: "find-by-sku",
		Groups: []string{"products"}, PathParams: []genParam{}, Body: body,
	}
	if _, ok := singlePositionalField(withoutPath); !ok {
		t.Fatal("sin parámetros de ruta el atajo del campo único debe seguir existiendo")
	}
	if c := buildGeneratedCommand(withoutPath); c.Use != "find-by-sku [sku]" {
		t.Fatalf("Use = %q, want %q", c.Use, "find-by-sku [sku]")
	}

	withAxis := withoutPath
	withAxis.Path = "/companies/{company}/products/find-by-sku"
	withAxis.PathParams = []genParam{{Name: "company", In: "path", Type: "string", Required: true}}
	if _, ok := singlePositionalField(withAxis); ok {
		t.Fatal("con un parámetro de ruta el atajo NO debe existir: es lo que mantiene vacío el universo de la aridad ambigua")
	}

	// Y la intersección medida sobre el contrato que el binario lleva dentro.
	for _, op := range generatedOps() {
		if _, ok := singlePositionalField(op); !ok {
			continue
		}
		if len(op.PathParams) != 0 {
			t.Errorf("%s admite atajo de cuerpo con %d parámetros de ruta", op.OperationID, len(op.PathParams))
		}
		if op.companyPathParamIndex() >= 0 {
			t.Errorf("%s tendría a la vez slot de eje y atajo de cuerpo: la aridad sería ambigua", op.OperationID)
		}
	}
}
