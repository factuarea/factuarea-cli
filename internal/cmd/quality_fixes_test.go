package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

func scopedServer(t *testing.T, body []byte, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == identityPath() {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEnumFlagRejectedClientSide(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.URL.Path != identityPath() {
			t.Fatalf("la red NO debe tocarse con enum inválido: %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	_, err := runCmd(t, srv.URL, "quotes", "convert", "--company", "acme_co", "qt_1", "--target", "proforma", "--confirm", "qt_1")
	if err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("esperaba rechazo de enum, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want Usage", exit.ForError(err))
	}
}

func TestEnumFlagAcceptsValidValue(t *testing.T) {
	srv := scopedServer(t, []byte(`{"data":{"id":"inv_1"}}`), 200)
	if _, err := runCmd(t, srv.URL, "quotes", "convert", "--company", "acme_co", "qt_1", "--target", "invoice", "--confirm", "qt_1", "--json"); err != nil {
		t.Fatalf("target=invoice debe aceptarse: %v", err)
	}
}

func TestRequiredBodyFlagEnforced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.URL.Path != identityPath() {
			t.Fatalf("la red NO debe tocarse sin el campo requerido: %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	_, err := runCmd(t, srv.URL, "contacts", "create", "--company", "acme_co", "--skip-scope-check", "--tax-id", "B1")
	if err == nil || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("esperaba campo requerido --name, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want Usage", exit.ForError(err))
	}
}

func TestRequiredBodyFlagSatisfiedByRawData(t *testing.T) {
	srv := scopedServer(t, []byte(`{"data":{"id":"0199152d-525d-7000-8000-000000000001"}}`), 200)
	if _, err := runCmd(t, srv.URL, "contacts", "create", "--company", "acme_co", "--skip-scope-check", "-d", `{"name":"X"}`, "--json"); err != nil {
		t.Fatalf("-d con el cuerpo completo debe satisfacer requeridos: %v", err)
	}
}

func TestByTypeRequiresType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.URL.Path != identityPath() {
			t.Fatalf("la red NO debe tocarse sin --type: %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	_, err := runCmd(t, srv.URL, "taxes", "by-type", "--company", "acme_co", "--skip-scope-check")
	if err == nil || !strings.Contains(err.Error(), "type") {
		t.Fatalf("by-type debe exigir --type, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want Usage", exit.ForError(err))
	}
}

func TestDataAndDataFileRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.URL.Path != identityPath() {
			t.Fatalf("la red NO debe tocarse: %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	_, err := runCmd(t, srv.URL, "contacts", "create", "--company", "acme_co", "--skip-scope-check", "-d", `{"name":"X"}`, "--data-file", "/tmp/x.json")
	if err == nil || !strings.Contains(err.Error(), "--data-file") {
		t.Fatalf("esperaba rechazo de doble fuente de cuerpo, got %v", err)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("exit = %d, want Usage", exit.ForError(err))
	}
}

// TestFindBySkuTravelsInTheBodyUnderTheAxis: con el eje, el ÚNICO posicional de
// `find-by-sku` es la empresa y el valor buscado viaja por su flag de cuerpo.
func TestFindBySkuTravelsInTheBodyUnderTheAxis(t *testing.T) {
	var gotBody map[string]any
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == identityPath() {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
			return
		}
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"prod_1"}}`))
	}))
	t.Cleanup(srv.Close)
	if _, err := runCmd(t, srv.URL, "products", "find-by-sku", "acme_co", "--sku", "MI-SKU", "--json"); err != nil {
		t.Fatalf("find-by-sku con el eje posicional debe funcionar: %v", err)
	}
	if gotBody["sku"] != "MI-SKU" {
		t.Fatalf("--sku debe viajar en el cuerpo: %v", gotBody)
	}
	if want := "/v1/companies/acme_co/products/find-by-sku"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
}

// TestFindByNoLongerOffersThePositionalBodyShortcut: el atajo del campo único
// de cuerpo exige CERO parámetros de ruta, y el eje le da uno a toda operación
// de empresa. El posicional que queda es el de la empresa, no el del campo, y
// la ayuda tiene que decirlo: si volviera a anunciarse `[sku]`, la misma
// invocación significaría dos cosas distintas según la operación.
func TestFindByNoLongerOffersThePositionalBodyShortcut(t *testing.T) {
	c := findCommand(t, "products", "find-by-sku")
	if want := "find-by-sku [company]"; c.Use != want {
		t.Fatalf("Use = %q, want %q", c.Use, want)
	}
	if strings.Contains(c.Use, "[sku]") {
		t.Errorf("el atajo posicional del campo único NO puede convivir con el eje: %q", c.Use)
	}
}

// TestCompanyGivenTwiceIsRejected: el eje aportado por las dos vías es un error
// de uso, no una precedencia silenciosa.
func TestCompanyGivenTwiceIsRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("la red NO debe tocarse: %s", r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	_, err := runCmd(t, srv.URL, "products", "find-by-sku", "acme_co", "--company", "otra_co", "--sku", "B", "--skip-scope-check")
	if err == nil || exit.ForError(err) != exit.Usage {
		t.Fatalf("esperaba conflicto posicional+--company (Usage), got %v", err)
	}
	if !strings.Contains(err.Error(), "las dos vías") {
		t.Fatalf("el fallo debe nombrar las dos vías: %v", err)
	}
}

func TestDeleteEmitsConfirmationJSON(t *testing.T) {
	srv := scopedServer(t, nil, 204)
	out, err := runCmd(t, srv.URL, "contacts", "delete", "--company", "acme_co", "con_42", "--confirm", "con_42", "--json")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	var payload map[string]any
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(out)), &payload); jerr != nil {
		t.Fatalf("delete --json debe emitir JSON: %q (%v)", out, jerr)
	}
	if payload["deleted"] != true || payload["id"] != "con_42" {
		t.Fatalf("delete --json debe confirmar {deleted,id}: %v", payload)
	}
}

func TestMutatingDerivedFromScope(t *testing.T) {
	out, err := runCmd(t, "", "commands", "--json")
	if err != nil {
		t.Fatalf("commands: %v", err)
	}
	var manifest []map[string]any
	if jerr := json.Unmarshal([]byte(out), &manifest); jerr != nil {
		t.Fatalf("manifest no es JSON: %v", jerr)
	}
	mutByCmd := map[string]bool{}
	for _, e := range manifest {
		mutByCmd[e["command"].(string)] = e["mutating"] == true
	}
	for _, read := range []string{
		"factuarea contacts find-by-tax-id",
		"factuarea products find-by-sku",
		"factuarea taxes calculate",
	} {
		if v, ok := mutByCmd[read]; ok && v {
			t.Errorf("%q es una lectura (:read), mutating debe ser false", read)
		}
	}
	if !mutByCmd["factuarea contacts create"] {
		t.Error("contacts create debe ser mutating")
	}
}

func TestCobraArgErrorTranslated(t *testing.T) {
	_, err := runCmd(t, "", "contacts", "show")
	if err == nil {
		t.Fatal("esperaba error de argumentos")
	}
	if strings.Contains(err.Error(), "accepts") || strings.Contains(err.Error(), "arg(s)") {
		t.Fatalf("el mensaje de cobra debe traducirse al español: %v", err)
	}
}

func TestNonJSONErrorBodyNormalized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == identityPath() {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"api_key":{"scopes":["*"]}}}`))
			return
		}
		w.Header().Set("Server", "Apache/2.4.67 (Debian)")
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(400)
		_, _ = w.Write([]byte("<html><body>Bad Request: malformed input</body></html>"))
	}))
	t.Cleanup(srv.Close)
	_, err := runCmd(t, srv.URL, "contacts", "show", "--company", "acme_co", "con_1")
	if err == nil {
		t.Fatal("una respuesta de error no-JSON debe producir error")
	}
	if !strings.Contains(err.Error(), "respuesta no-JSON") {
		t.Fatalf("el error no-JSON debe normalizarse, got %v", err)
	}
	if strings.Contains(err.Error(), "malformed input") || strings.Contains(err.Error(), "Apache") || strings.Contains(err.Error(), "html") {
		t.Fatalf("el cuerpo/HTML crudo NO debe volcarse (disclosure), got %v", err)
	}
}

func TestTriggerListJSON(t *testing.T) {
	out, err := runCmd(t, "", "trigger", "--list", "--json")
	if err != nil {
		t.Fatalf("trigger --list: %v", err)
	}
	var events []string
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(out)), &events); jerr != nil {
		t.Fatalf("trigger --list --json debe emitir un array JSON: %q (%v)", out, jerr)
	}
	if len(events) == 0 {
		t.Fatal("trigger --list debe listar eventos")
	}
}

func TestTriggerRejectsMalformedOverride(t *testing.T) {
	_, err := runCmd(t, "", "trigger", "contact.created", "--override", "novalue")
	if err == nil || !strings.Contains(err.Error(), "k=v") {
		t.Fatalf("esperaba validación de formato k=v, got %v", err)
	}
}

func TestLogoutJSON(t *testing.T) {
	out, err := runCmd(t, "", "logout", "--json")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	var payload map[string]any
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(out)), &payload); jerr != nil {
		t.Fatalf("logout --json debe emitir JSON: %q (%v)", out, jerr)
	}
	if payload["logged_out"] != true {
		t.Fatalf("logout --json debe confirmar logged_out: %v", payload)
	}
}

func TestPlainFlagRemoved(t *testing.T) {
	_, err := runCmd(t, "", "contacts", "list", "--company", "acme_co", "--plain")
	if err == nil || !strings.Contains(err.Error(), "flag desconocido") {
		t.Fatalf("--plain debe estar retirado (flag desconocido), got %v", err)
	}
}
