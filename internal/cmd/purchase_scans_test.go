package cmd

import (
	"bytes"
	"encoding/json"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// purchase_scans_test.go es el gate ÚNICO del escáner de compras (`design.md`
// D7, precedente `automation-cli-surface`): en vez de un test por comando,
// comprueba el conjunto EXACTO de las 13 operaciones del escáner contra
// `anchors/contract.md` de `scanner-sdk-cli-spec-sync`, así que un descarte
// del generador (`internal/gen/main.go`) o un cambio de contrato sin
// intención lo cazan aquí, no en un test disperso.

// purchaseScanOperationIDs son las 13 operaciones congeladas en
// `anchors/contract.md` §"Las 13 operaciones". El orden no importa: (a)
// compara conjuntos.
var purchaseScanOperationIDs = []string{
	"public-api.v1.purchase_scans.list",
	"public-api.v1.purchase_scans.create",
	"public-api.v1.purchase_scans.stats",
	"public-api.v1.purchase_scans.show",
	"public-api.v1.purchase_scans.source",
	"public-api.v1.purchase_scans.retry",
	"public-api.v1.purchase_scans.review",
	"public-api.v1.purchase_scans.duplicate_resolution",
	"public-api.v1.purchase_scans.convert",
	"public-api.v1.purchase_scans.archive",
	"public-api.v1.purchase_scans.restore",
	"public-api.v1.purchase_scan_emails.list",
	"public-api.v1.purchase_invoices.expense_categories",
}

// purchaseScanExpectedCommand es el comando CLI esperado por operation_id,
// derivado de `Resolve()` (`internal/spec/namespace.go`) sobre el
// `operationId`, igual que la tabla CLI de `design.md`.
var purchaseScanExpectedCommand = map[string]string{
	"public-api.v1.purchase_scans.list":                  "factuarea purchase-scans list",
	"public-api.v1.purchase_scans.create":                "factuarea purchase-scans create",
	"public-api.v1.purchase_scans.stats":                 "factuarea purchase-scans stats",
	"public-api.v1.purchase_scans.show":                  "factuarea purchase-scans show",
	"public-api.v1.purchase_scans.source":                "factuarea purchase-scans source",
	"public-api.v1.purchase_scans.retry":                 "factuarea purchase-scans retry",
	"public-api.v1.purchase_scans.review":                "factuarea purchase-scans review",
	"public-api.v1.purchase_scans.duplicate_resolution":  "factuarea purchase-scans duplicate-resolution",
	"public-api.v1.purchase_scans.convert":               "factuarea purchase-scans convert",
	"public-api.v1.purchase_scans.archive":               "factuarea purchase-scans archive",
	"public-api.v1.purchase_scans.restore":               "factuarea purchase-scans restore",
	"public-api.v1.purchase_scan_emails.list":            "factuarea purchase-scan-emails list",
	"public-api.v1.purchase_invoices.expense_categories": "factuarea purchase-invoices expense-categories",
}

// purchaseScanExpectedScope y purchaseScanExpectedIrreversible: `anchors/contract.md`
// §"Las 13 operaciones". Irreversible = true SOLO en duplicate_resolution y
// convert (`OpenApiOperationScopeRegistry.php:415-418`).
var purchaseScanExpectedScope = map[string]string{
	"public-api.v1.purchase_scans.list":                  "purchase_invoices:read",
	"public-api.v1.purchase_scans.create":                "purchase_invoices:write",
	"public-api.v1.purchase_scans.stats":                 "purchase_invoices:read",
	"public-api.v1.purchase_scans.show":                  "purchase_invoices:read",
	"public-api.v1.purchase_scans.source":                "purchase_invoices:read",
	"public-api.v1.purchase_scans.retry":                 "purchase_invoices:write",
	"public-api.v1.purchase_scans.review":                "purchase_invoices:write",
	"public-api.v1.purchase_scans.duplicate_resolution":  "purchase_invoices:write",
	"public-api.v1.purchase_scans.convert":               "purchase_invoices:write",
	"public-api.v1.purchase_scans.archive":               "purchase_invoices:delete",
	"public-api.v1.purchase_scans.restore":               "purchase_invoices:write",
	"public-api.v1.purchase_scan_emails.list":            "purchase_invoices:read",
	"public-api.v1.purchase_invoices.expense_categories": "purchase_invoices:read",
}

var purchaseScanExpectedIrreversible = map[string]bool{
	"public-api.v1.purchase_scans.duplicate_resolution": true,
	"public-api.v1.purchase_scans.convert":              true,
}

var purchaseScanExpectedPaginated = map[string]bool{
	"public-api.v1.purchase_scans.list":       true,
	"public-api.v1.purchase_scan_emails.list": true,
}

func TestPurchaseScanManifestExactSetAndCommands(t *testing.T) {
	out, err := runCmd(t, "", "commands", "--json")
	if err != nil {
		t.Fatalf("commands --json: %v", err)
	}
	var manifest []map[string]any
	if err := json.Unmarshal([]byte(out), &manifest); err != nil {
		t.Fatalf("manifest no es JSON: %v", err)
	}

	byOpID := map[string]map[string]any{}
	for _, e := range manifest {
		opID, _ := e["operation_id"].(string)
		if strings.HasPrefix(opID, "public-api.v1.purchase_scan") ||
			opID == "public-api.v1.purchase_invoices.expense_categories" {
			byOpID[opID] = e
		}
	}

	// (a) el conjunto EXACTO de los 13 operation_id: ni de menos (un
	// descarte silencioso del generador) ni de más (una operación que
	// tasks.md no congeló en anchors/contract.md).
	var missing []string
	for _, id := range purchaseScanOperationIDs {
		if _, ok := byOpID[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("faltan operation_id del escáner en el manifiesto (descarte del generador): %v", missing)
	}
	if len(byOpID) != len(purchaseScanOperationIDs) {
		var extra []string
		for id := range byOpID {
			found := false
			for _, want := range purchaseScanOperationIDs {
				if id == want {
					found = true
					break
				}
			}
			if !found {
				extra = append(extra, id)
			}
		}
		t.Fatalf("operation_id del escáner de más en el manifiesto (no congelados en anchors/contract.md): %v", extra)
	}

	for id, wantCommand := range purchaseScanExpectedCommand {
		e := byOpID[id]
		if got, _ := e["command"].(string); got != wantCommand {
			t.Errorf("%s: command = %q, want %q", id, got, wantCommand)
		}
	}
}

func TestPurchaseScanScopeAndIrreversible(t *testing.T) {
	out, err := runCmd(t, "", "commands", "--json")
	if err != nil {
		t.Fatalf("commands --json: %v", err)
	}
	var manifest []map[string]any
	if err := json.Unmarshal([]byte(out), &manifest); err != nil {
		t.Fatalf("manifest no es JSON: %v", err)
	}
	byOpID := map[string]map[string]any{}
	for _, e := range manifest {
		opID, _ := e["operation_id"].(string)
		byOpID[opID] = e
	}

	for id, wantScope := range purchaseScanExpectedScope {
		e, ok := byOpID[id]
		if !ok {
			t.Fatalf("%s ausente del manifiesto", id)
		}
		if got, _ := e["required_scope"].(string); got != wantScope {
			t.Errorf("%s: required_scope = %q, want %q", id, got, wantScope)
		}
		wantIrr := purchaseScanExpectedIrreversible[id]
		gotIrr, _ := e["irreversible"].(bool)
		if gotIrr != wantIrr {
			t.Errorf("%s: irreversible = %v, want %v", id, gotIrr, wantIrr)
		}
	}
}

func TestPurchaseScanPaginatedAndBinary(t *testing.T) {
	out, err := runCmd(t, "", "commands", "--json")
	if err != nil {
		t.Fatalf("commands --json: %v", err)
	}
	var manifest []map[string]any
	if err := json.Unmarshal([]byte(out), &manifest); err != nil {
		t.Fatalf("manifest no es JSON: %v", err)
	}
	byOpID := map[string]map[string]any{}
	for _, e := range manifest {
		opID, _ := e["operation_id"].(string)
		byOpID[opID] = e
	}

	for _, id := range purchaseScanOperationIDs {
		e, ok := byOpID[id]
		if !ok {
			t.Fatalf("%s ausente del manifiesto", id)
		}
		wantPaginated := purchaseScanExpectedPaginated[id]
		gotPaginated, _ := e["paginated"].(bool)
		if gotPaginated != wantPaginated {
			t.Errorf("%s: paginated = %v, want %v", id, gotPaginated, wantPaginated)
		}
		wantBinary := id == "public-api.v1.purchase_scans.source"
		gotBinary, _ := e["binary"].(bool)
		if gotBinary != wantBinary {
			t.Errorf("%s: binary = %v, want %v", id, gotBinary, wantBinary)
		}
	}
}

// (d) `purchase-scans create` con dos ficheros bajo el campo congelado en
// `anchors/contract.md` §"Subida multipart" (`files[]`): dos partes, una por
// fichero, en el orden recibido.
func TestPurchaseScansCreateUploadsFileArray(t *testing.T) {
	var gotFieldNames, gotFilenames []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/purchase_scans" || r.Method != http.MethodPost {
			t.Errorf("request inesperada: %s %s", r.Method, r.URL.Path)
		}
		_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("Content-Type: %v", err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			gotFieldNames = append(gotFieldNames, part.FormName())
			gotFilenames = append(gotFilenames, part.FileName())
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"data":{"accepted":[],"rejected":[]}}`))
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	a := filepath.Join(dir, "a.pdf")
	b := filepath.Join(dir, "b.png")
	if err := os.WriteFile(a, []byte("PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("PNG"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := runCmd(t, srv.URL, "purchase-scans", "create",
		"--file-files", a, "--file-files", b, "--skip-scope-check", "--json")
	if err != nil {
		t.Fatalf("purchase-scans create: %v", err)
	}

	if len(gotFieldNames) != 2 {
		t.Fatalf("esperaba 2 partes, got %d (%v)", len(gotFieldNames), gotFieldNames)
	}
	for _, n := range gotFieldNames {
		if n != "files[]" {
			t.Errorf("nombre de campo = %q, want files[] (congelado en anchors/contract.md §1.4): %v", n, gotFieldNames)
		}
	}
	if gotFilenames[0] != "a.pdf" || gotFilenames[1] != "b.png" {
		t.Errorf("filenames = %v, want [a.pdf b.png] (orden recibido)", gotFilenames)
	}
}

// (e) `purchase-scans archive` exige `Idempotency-Key`: la mide como
// requerida `anchors/contract.md` §"Las 13 operaciones" y §1.5. El cliente
// (`internal/client/client.go`) ya la autogenera para todo verbo mutador,
// así que esto comprueba el comportamiento observable con independencia de
// si la genera el flujo D6 (`build_generated.go`) o el blanket de `client.Do`.
func TestPurchaseScansArchiveSendsIdempotencyKey(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("método inesperado: %s", r.Method)
		}
		gotKey = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"01928f10-0000-7000-8000-000000000001","status":"archived"}}`))
	}))
	t.Cleanup(srv.Close)

	_, err := runCmd(t, srv.URL, "purchase-scans", "archive",
		"01928f10-0000-7000-8000-000000000001", "-d", `{"expected_version":1}`, "--skip-scope-check", "--json")
	if err != nil {
		t.Fatalf("purchase-scans archive: %v", err)
	}
	if strings.TrimSpace(gotKey) == "" {
		t.Error("esperaba Idempotency-Key en el archive del escáner (requerida en anchors/contract.md)")
	}
}

// (f) `purchase-scans list --json` produce JSON válido en stdout, en un
// buffer separado de stderr.
func TestPurchaseScansListJSONOnStdoutOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/purchase_scans" || r.Method != http.MethodGet {
			t.Errorf("request inesperada: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"has_more":false,"next_cursor":null}`))
	}))
	t.Cleanup(srv.Close)

	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", srv.URL)
	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"purchase-scans", "list", "--skip-scope-check", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("purchase-scans list: %v (stderr: %s)", err, stderr.String())
	}

	if stderr.Len() != 0 {
		t.Errorf("stderr no debe llevar la respuesta: %q", stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout no es JSON válido: %v (stdout: %q)", err, stdout.String())
	}
	if _, ok := payload["data"]; !ok {
		t.Errorf("stdout sin el envelope data: %v", payload)
	}
}

func TestPurchaseScanEmailsScalarAndArrayResultFilters(t *testing.T) {
	for _, tc := range []struct {
		flag  string
		value string
		key   string
		want  []string
	}{
		{"--result", "accepted", "result", []string{"accepted"}},
		{"--result[]", "accepted,rejected", "result[]", []string{"accepted", "rejected"}},
	} {
		t.Run(tc.flag, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got := r.URL.Query()[tc.key]
				if strings.Join(got, ",") != strings.Join(tc.want, ",") {
					t.Errorf("query %s = %v; want %v", tc.key, got, tc.want)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[],"has_more":false,"next_cursor":null}`))
			}))
			t.Cleanup(srv.Close)
			if _, err := runCmd(t, srv.URL, "purchase-scan-emails", "list", "--skip-scope-check", tc.flag, tc.value, "--json"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
