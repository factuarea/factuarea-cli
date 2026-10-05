package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/factuarea/factuarea-cli/internal/exit"
)

// Las pruebas de este fichero cubren el EJE DE CUENTA de la CLI (`design.md`
// D4 y D5 del change `account-tenancy-sibling-repos`). Las operaciones salen de
// la sección C del inventario de sincronización: 71 bajo `/accounts`, 27
// lecturas con slot, 30 escrituras con slot, 3 irreversibles con el slot como
// último path param, 6 recursos de `managed-accounts` y 5 raíz.

const (
	ownAccountID  = "01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8c01"
	otherAccount  = "01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8c02"
	positionalAcc = "01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8c03"
	flagAcc       = "01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8c04"
	managedAcc    = "01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8c05"
	someInvite    = "01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8c06"
)

// identityWithAccount es la identidad que publica `data.account.id`.
const identityWithAccount = `{"data":{"account":{"id":"` + ownAccountID + `"},"api_key":{"scopes":["*"]}}}`

// accountRecorder anota cada petición que llega al servidor de pruebas.
type accountRecorder struct {
	mu   sync.Mutex
	reqs []string
}

func (r *accountRecorder) add(method, path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, method+" "+path)
}

func (r *accountRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.reqs...)
}

func (r *accountRecorder) identityHits() int {
	n := 0
	for _, q := range r.all() {
		if q == "GET "+identityPath() {
			n++
		}
	}
	return n
}

// accountServer sirve la identidad con cuenta y responde 200 a cualquier otra
// ruta, anotándola.
func accountServer(t *testing.T, identity string) (*httptest.Server, *accountRecorder) {
	t.Helper()
	rec := &accountRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == identityPath() {
			_, _ = w.Write([]byte(identity))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// accountTestEnv fija la credencial, la URL base y vacía las dos memorias de
// resolución, que son POR PROCESO.
func accountTestEnv(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("FACTUAREA_API_KEY", "fact_test_aaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("FACTUAREA_BASE_URL", baseURL)
	t.Cleanup(resetAccountResolution)
	t.Cleanup(resetCompanyResolution)
	resetAccountResolution()
	resetCompanyResolution()
}

// runAccountRoot ejecuta el árbol REAL de comandos.
func runAccountRoot(t *testing.T, baseURL string, args ...string) (string, error) {
	t.Helper()
	accountTestEnv(t, baseURL)
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// accountOp devuelve la operación del contrato embebido por su operationId.
func accountOp(t *testing.T, operationID string) genOp {
	t.Helper()
	for _, op := range generatedOps() {
		if op.OperationID == operationID {
			return op
		}
	}
	t.Fatalf("el contrato embebido no declara la operación %s", operationID)
	return genOp{}
}

func requireUsageError(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s debe terminar con error", what)
	}
	if exit.ForError(err) != exit.Usage {
		t.Fatalf("%s: exit = %d, want %d (error de uso): %v", what, exit.ForError(err), exit.Usage, err)
	}
}

// TestAccountAxisClassificationFollowsInventoryC mide las 74 operaciones bajo
// `/accounts` del documento embebido contra las cinco clases de la tabla.
func TestAccountAxisClassificationFollowsInventoryC(t *testing.T) {
	var total, root, resource, explicit, read, write int
	for _, op := range generatedOps() {
		if op.Path != "/accounts" && !strings.HasPrefix(op.Path, "/accounts/") {
			continue
		}
		total++
		switch {
		case !op.isAccountAxisPath():
			root++
			if len(op.PathParams) != 0 || op.accountPathParamIndex() != -1 || op.accountFillIndex() != -1 {
				t.Errorf("%s (%s) es raíz y no debe tener slot de cuenta", op.OperationID, op.Path)
			}
		case op.accountPathParamIndex() < 0:
			resource++
			if op.Groups[0] != managedAccountsGroup || op.PathParams[0].Name != accountPathParam {
				t.Errorf("%s: solo el grupo %s es recurso", op.OperationID, managedAccountsGroup)
			}
			if op.accountFillIndex() != -1 {
				t.Errorf("%s: la cuenta gestionada nunca se rellena", op.OperationID)
			}
		case op.requiresExplicitAccount():
			explicit++
			if op.accountFillIndex() != -1 || !op.Irreversible {
				t.Errorf("%s: irreversible con el slot como último path param no admite relleno", op.OperationID)
			}
		case op.autoResolvesAccount():
			read++
			if op.accountFillIndex() != 0 {
				t.Errorf("%s: la lectura con slot debe poder rellenar la posición 0", op.OperationID)
			}
		default:
			write++
			if op.accountFillIndex() != 0 {
				t.Errorf("%s: la escritura con slot debe poder rellenar la posición 0", op.OperationID)
			}
		}
	}
	got := [6]int{total, read, write, explicit, resource, root}
	want := [6]int{74, 28, 32, 3, 6, 5}
	if got != want {
		t.Fatalf("[total lectura escritura irreversible recurso raíz] = %v, want %v (sección C del inventario)", got, want)
	}

	for _, id := range []string{
		"public-api.v1.account.nif_transfers.create",
		"public-api.v1.account.nif_transfers.accept",
		"public-api.v1.account.owner.transfer",
	} {
		if !accountOp(t, id).requiresExplicitAccount() {
			t.Errorf("%s debe exigir la cuenta explícita", id)
		}
	}
	for _, id := range []string{
		"public-api.v1.managed_accounts.show",
		"public-api.v1.managed_accounts.access_level.update",
		"public-api.v1.managed_accounts.claim_tokens.create",
		"public-api.v1.managed_accounts.management.end",
		"public-api.v1.managed_accounts.plan.update",
		"public-api.v1.managed_accounts.plan.preview",
	} {
		if accountOp(t, id).accountPathParamIndex() != -1 {
			t.Errorf("%s es recurso: no tiene slot de eje de cuenta", id)
		}
	}
	// Los hermanos de empresa bajo la cuenta SÍ tienen slot, y `{company}` es recurso.
	del := accountOp(t, "public-api.v1.companies.delete")
	if del.accountFillIndex() != 0 || del.companyPathParamIndex() != -1 {
		t.Errorf("companies delete: slot de cuenta en 0 y {company} como recurso; got cuenta=%d empresa=%d",
			del.accountFillIndex(), del.companyPathParamIndex())
	}
	if accountOp(t, "public-api.v1.account.provisioning_usage").accountFillIndex() != 0 {
		t.Error("account provisioning-usage es de la cuenta propia y sí tiene slot")
	}
}

// TestAccountReadResolvesOwnAccountAndGoesToItsRoute: una lectura sin cuenta la
// resuelve sola.
func TestAccountReadResolvesOwnAccountAndGoesToItsRoute(t *testing.T) {
	srv, rec := accountServer(t, identityWithAccount)
	_, err := runAccountRoot(t, srv.URL, "account", "members", "list", "--skip-scope-check")
	if err != nil {
		t.Fatalf("la lectura sin cuenta debe resolverla sola: %v", err)
	}
	want := []string{"GET " + identityPath(), "GET /v1/accounts/" + ownAccountID + "/members"}
	if got := rec.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("peticiones = %v, want %v", got, want)
	}
}

// TestAccountWriteWithoutAccountIsAUsageErrorAndSendsNothing: una escritura sin
// cuenta no se envía y el mensaje nombra la cuenta propia y las dos vías.
func TestAccountWriteWithoutAccountIsAUsageErrorAndSendsNothing(t *testing.T) {
	srv, rec := accountServer(t, identityWithAccount)
	_, err := runAccountRoot(t, srv.URL, "account", "invitations", "resend", someInvite, "--skip-scope-check")
	requireUsageError(t, err, "una escritura sin cuenta")
	for _, want := range []string{ownAccountID, "--account", "argumento posicional", "<account>"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("el mensaje debe contener %q; got:\n%v", want, err)
		}
	}
	for _, req := range rec.all() {
		if strings.HasPrefix(req, "POST ") {
			t.Fatalf("la escritura NO debía enviarse: %v", rec.all())
		}
	}
	if got := rec.identityHits(); got != 1 {
		t.Fatalf("lecturas de identidad = %d, want 1 (solo para nombrar la cuenta propia)", got)
	}
}

// TestAccountFlagFillsAWrite: el flag rellena la escritura y no hace falta
// consultar la identidad.
func TestAccountFlagFillsAWrite(t *testing.T) {
	srv, rec := accountServer(t, identityWithAccount)
	_, err := runAccountRoot(t, srv.URL, "account", "invitations", "resend", someInvite,
		"--account", otherAccount, "--skip-scope-check")
	if err != nil {
		t.Fatalf("la escritura con --account debe enviarse: %v", err)
	}
	want := []string{"POST /v1/accounts/" + otherAccount + "/invitations/" + someInvite + "/resend"}
	if got := rec.all(); !reflect.DeepEqual(got, want) {
		t.Fatalf("peticiones = %v, want %v", got, want)
	}
}

// TestAccountPositionalWinsAndBothWaysIsAnError: el posicional gana y no
// consulta nada; los dos a la vez es error de uso.
func TestAccountPositionalWinsAndBothWaysIsAnError(t *testing.T) {
	t.Run("el posicional gana y no consulta la identidad", func(t *testing.T) {
		srv, rec := accountServer(t, identityWithAccount)
		_, err := runAccountRoot(t, srv.URL, "account", "invitations", "resend", positionalAcc, someInvite, "--skip-scope-check")
		if err != nil {
			t.Fatalf("el posicional debe valer: %v", err)
		}
		want := []string{"POST /v1/accounts/" + positionalAcc + "/invitations/" + someInvite + "/resend"}
		if got := rec.all(); !reflect.DeepEqual(got, want) {
			t.Fatalf("peticiones = %v, want %v", got, want)
		}
	})

	t.Run("las dos vías en una escritura son error de uso", func(t *testing.T) {
		srv, rec := accountServer(t, identityWithAccount)
		_, err := runAccountRoot(t, srv.URL, "account", "invitations", "resend", positionalAcc, someInvite,
			"--account", flagAcc, "--skip-scope-check")
		requireUsageError(t, err, "la cuenta por posicional y por flag")
		for _, want := range []string{"--account", positionalAcc, flagAcc} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("el mensaje debe nombrar el conflicto (%q); got:\n%v", want, err)
			}
		}
		if got := rec.all(); len(got) != 0 {
			t.Fatalf("no debía enviarse nada: %v", got)
		}
	})

	t.Run("las dos vías en una lectura son error de uso", func(t *testing.T) {
		srv, rec := accountServer(t, identityWithAccount)
		_, err := runAccountRoot(t, srv.URL, "account", "members", "list", positionalAcc,
			"--account", flagAcc, "--skip-scope-check")
		requireUsageError(t, err, "la cuenta por posicional y por flag en una lectura")
		if got := rec.all(); len(got) != 0 {
			t.Fatalf("no debía enviarse nada: %v", got)
		}
	})
}

// TestAccountIrreversibleWithSlotAsLastParamDemandsThePositional: ni el flag
// vale; hay que escribir el posicional.
func TestAccountIrreversibleWithSlotAsLastParamDemandsThePositional(t *testing.T) {
	for _, tc := range []struct {
		operationID string
		command     []string
	}{
		{"public-api.v1.account.owner.transfer", []string{"account", "owner", "transfer"}},
		{"public-api.v1.account.nif_transfers.accept", []string{"account", "nif-transfers", "accept"}},
		{"public-api.v1.account.nif_transfers.create", []string{"account", "nif-transfers", "create"}},
	} {
		t.Run(tc.operationID, func(t *testing.T) {
			op := accountOp(t, tc.operationID)
			if !op.requiresExplicitAccount() || op.accountFillIndex() != -1 {
				t.Fatalf("%s: requiresExplicitAccount=%v accountFillIndex=%d", tc.operationID,
					op.requiresExplicitAccount(), op.accountFillIndex())
			}
			if c := buildGeneratedCommand(op); !strings.Contains(c.Use, "<account>") || strings.Contains(c.Use, "[account]") {
				t.Fatalf("Use = %q: el slot se muestra obligatorio", c.Use)
			}

			srv, rec := accountServer(t, identityWithAccount)
			args := append(append([]string{}, tc.command...), "--account", flagAcc, "--skip-scope-check")
			_, err := runAccountRoot(t, srv.URL, args...)
			requireUsageError(t, err, "una irreversible sin el posicional")
			for _, want := range []string{"irreversible", "--account"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("el mensaje debe decir por qué no se rellena (%q): %v", want, err)
				}
			}
			if got := rec.all(); len(got) != 0 {
				t.Fatalf("no debía enviarse nada: %v", got)
			}

			// Con el posicional escrito el flag no interviene.
			pathValues, _, serr := op.splitAxisPositionals([]string{positionalAcc}, axisValues{Account: flagAcc})
			if serr != nil || len(pathValues) != 1 || pathValues[0] != positionalAcc {
				t.Fatalf("con el posicional, path = %v err = %v; want [%s]", pathValues, serr, positionalAcc)
			}
			got, rerr := op.resolveAccountForArgs(context.Background(), &GlobalFlags{Account: flagAcc}, nil)
			if rerr != nil || got != "" {
				t.Fatalf("resolveAccountForArgs = %q, %v: una irreversible con slot final no resuelve nada", got, rerr)
			}
		})
	}
}

// TestManagedAccountsNeverFillTheManagedAccount: la cuenta gestionada es un
// recurso, el flag no la rellena.
func TestManagedAccountsNeverFillTheManagedAccount(t *testing.T) {
	t.Run("sin el identificador falla aunque el flag esté puesto y no consulta nada", func(t *testing.T) {
		for _, args := range [][]string{
			{"managed-accounts", "management", "end"},
			{"managed-accounts", "show"},
			{"managed-accounts", "plan", "preview"},
		} {
			srv, rec := accountServer(t, identityWithAccount)
			full := append(append([]string{}, args...), "--account", flagAcc, "--skip-scope-check")
			_, err := runAccountRoot(t, srv.URL, full...)
			if err == nil {
				t.Fatalf("%v sin la cuenta gestionada debe fallar", args)
			}
			if got := rec.all(); len(got) != 0 {
				t.Fatalf("%v: no debía enviarse ni consultarse nada: %v", args, got)
			}
		}
	})

	t.Run("con el identificador escrito el flag no interviene", func(t *testing.T) {
		srv, rec := accountServer(t, identityWithAccount)
		_, err := runAccountRoot(t, srv.URL, "managed-accounts", "show", managedAcc, "--account", flagAcc, "--skip-scope-check")
		if err != nil {
			t.Fatalf("managed-accounts show <cuenta>: %v", err)
		}
		want := []string{"GET /v1/accounts/" + managedAcc}
		if got := rec.all(); !reflect.DeepEqual(got, want) {
			t.Fatalf("peticiones = %v, want %v", got, want)
		}
	})

	t.Run("la ayuda muestra el identificador obligatorio", func(t *testing.T) {
		c := buildGeneratedCommand(accountOp(t, "public-api.v1.managed_accounts.show"))
		if c.Use != "show <account>" {
			t.Fatalf("Use = %q, want %q", c.Use, "show <account>")
		}
	})

	for _, id := range []string{
		"public-api.v1.managed_accounts.show",
		"public-api.v1.managed_accounts.management.end",
		"public-api.v1.managed_accounts.plan.update",
	} {
		op := accountOp(t, id)
		got, err := op.resolveAccountForArgs(context.Background(), &GlobalFlags{Account: flagAcc}, nil)
		if err != nil || got != "" {
			t.Errorf("%s: resolveAccountForArgs = %q, %v; la cuenta gestionada nunca se resuelve", id, got, err)
		}
	}
}

// TestAccountRootProvisioningRoutesCarryNoAccount: las rutas raíz no llevan
// cuenta aunque el flag esté puesto.
func TestAccountRootProvisioningRoutesCarryNoAccount(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cuentas.csv")
	if err := os.WriteFile(file, []byte("external_ref,tax_id\nc1,B12345674\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	createBody := `{"external_ref":"cliente-1","plan":"emprendedor","tax_id":"B12345674","legal_name":"Talleres SL"}`

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"alta de cuenta gestionada", []string{"managed-accounts", "create", "--data", createBody, "--confirm", "create"}, "POST /v1/accounts"},
		{"vista previa de la importación", []string{"managed-accounts", "imports", "preview", "--file-accounts_file", file}, "POST /v1/accounts/imports/preview"},
		{"plantilla de la importación", []string{"managed-accounts", "imports", "template"}, "GET /v1/accounts/imports/template"},
		{"listado", []string{"managed-accounts", "list"}, "GET /v1/accounts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, rec := accountServer(t, identityWithAccount)
			args := append(append([]string{}, tc.args...), "--account", flagAcc, "--skip-scope-check")
			if _, err := runAccountRoot(t, srv.URL, args...); err != nil {
				t.Fatalf("%v: %v", tc.args, err)
			}
			if got := rec.all(); !reflect.DeepEqual(got, []string{tc.want}) {
				t.Fatalf("peticiones = %v, want [%s]: la ruta raíz no lleva segmento de cuenta", got, tc.want)
			}
		})
	}
}

// TestCompanyFlagDoesNotFillTheAccountSlot: `--company` no aporta la cuenta.
func TestCompanyFlagDoesNotFillTheAccountSlot(t *testing.T) {
	const company = "01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8a01"

	t.Run("una lectura resuelve la cuenta por su cadena", func(t *testing.T) {
		srv, rec := accountServer(t, identityWithAccount)
		if _, err := runAccountRoot(t, srv.URL, "account", "members", "list", "--company", company, "--skip-scope-check"); err != nil {
			t.Fatalf("la lectura con --company debe resolver la cuenta propia: %v", err)
		}
		want := []string{"GET " + identityPath(), "GET /v1/accounts/" + ownAccountID + "/members"}
		if got := rec.all(); !reflect.DeepEqual(got, want) {
			t.Fatalf("peticiones = %v, want %v: --company no es la cuenta", got, want)
		}
	})

	t.Run("una escritura sigue exigiendo nombrar la cuenta", func(t *testing.T) {
		srv, rec := accountServer(t, identityWithAccount)
		_, err := runAccountRoot(t, srv.URL, "account", "invitations", "resend", someInvite, "--company", company, "--skip-scope-check")
		requireUsageError(t, err, "una escritura de cuenta con solo --company")
		for _, req := range rec.all() {
			if strings.HasPrefix(req, "POST ") {
				t.Fatalf("la escritura NO debía enviarse: %v", rec.all())
			}
		}
	})

	t.Run("la resolución de cuenta ignora g.Company", func(t *testing.T) {
		accountTestEnv(t, "http://127.0.0.1:1")
		op := accountOp(t, "public-api.v1.account.invitations.resend")
		got, err := op.resolveAccountForArgs(context.Background(), &GlobalFlags{Company: company, Account: flagAcc}, nil)
		if err != nil || got != flagAcc {
			t.Fatalf("resolveAccountForArgs = %q, %v; want %q", got, err, flagAcc)
		}
	})
}

// TestAccountResolutionIsMemoizedPerProcessAndResetEmptiesIt.
func TestAccountResolutionIsMemoizedPerProcessAndResetEmptiesIt(t *testing.T) {
	srv, rec := accountServer(t, identityWithAccount)
	accountTestEnv(t, srv.URL)

	for i := range 3 {
		got, err := resolveAccount(context.Background(), &GlobalFlags{}, "", true)
		if err != nil || got != ownAccountID {
			t.Fatalf("resolución %d = %q, %v; want %q", i+1, got, err, ownAccountID)
		}
	}
	// Nombrar la cuenta propia en el error de una escritura reutiliza la memoria.
	if _, err := resolveAccount(context.Background(), &GlobalFlags{}, "", false); err == nil ||
		!strings.Contains(err.Error(), ownAccountID) {
		t.Fatalf("la escritura sin cuenta debe fallar nombrando %s: %v", ownAccountID, err)
	}
	if got := rec.identityHits(); got != 1 {
		t.Fatalf("lecturas de identidad = %d, want 1: el valor se memoiza por proceso", got)
	}

	resetAccountResolution()
	if _, err := resolveAccount(context.Background(), &GlobalFlags{}, "", true); err != nil {
		t.Fatal(err)
	}
	if got := rec.identityHits(); got != 2 {
		t.Fatalf("lecturas de identidad = %d, want 2 tras resetAccountResolution", got)
	}

	// Otra credencial es otra cuenta: la memoria va indexada por la credencial.
	t.Setenv("FACTUAREA_API_KEY", "fact_test_bbbbbbbbbbbbbbbbbbbbbbbb")
	if _, err := resolveAccount(context.Background(), &GlobalFlags{}, "", true); err != nil {
		t.Fatal(err)
	}
	if got := rec.identityHits(); got != 3 {
		t.Fatalf("lecturas de identidad = %d, want 3: la memoria no se comparte entre credenciales", got)
	}

	// Posicional y flag no consultan nada.
	if got, err := resolveAccount(context.Background(), &GlobalFlags{Account: flagAcc}, "", true); err != nil || got != flagAcc {
		t.Fatalf("con flag = %q, %v; want %q", got, err, flagAcc)
	}
	if got, err := resolveAccount(context.Background(), &GlobalFlags{}, positionalAcc, true); err != nil || got != "" {
		t.Fatalf("con posicional = %q, %v; want vacío (el reparto usa el posicional)", got, err)
	}
	if got := rec.identityHits(); got != 3 {
		t.Fatalf("posicional y flag no deben consultar la identidad: %d lecturas", got)
	}
}

// TestAccountMissingFromIdentityIsAUsageError: una identidad sin cuenta no
// permite deducirla.
func TestAccountMissingFromIdentityIsAUsageError(t *testing.T) {
	srv, rec := accountServer(t, `{"data":{"api_key":{"scopes":["*"]}}}`)
	_, err := runAccountRoot(t, srv.URL, "account", "members", "list", "--skip-scope-check")
	requireUsageError(t, err, "una lectura con identidad sin cuenta")
	for _, want := range []string{"falta la cuenta", "--account", "<account>"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("el mensaje debe contener %q: %v", want, err)
		}
	}
	for _, req := range rec.all() {
		if req != "GET "+identityPath() {
			t.Fatalf("solo debía leerse la identidad: %v", rec.all())
		}
	}
}

// TestAccountAxisHelpShowsTheAccountArgumentAndFlag: la ayuda de un comando
// del eje de cuenta muestra el argumento en primera posición y `--account`.
func TestAccountAxisHelpShowsTheAccountArgumentAndFlag(t *testing.T) {
	accountTestEnv(t, "http://127.0.0.1:1")
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"account", "members", "list", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("--help: %v", err)
	}
	help := out.String()
	for _, want := range []string{
		"factuarea account members list [account]",
		"--account string",
		"cuenta sobre la que operar",
		"Solo se deduce sola en las lecturas",
		"Forma corta:",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("la ayuda debe contener %q; got:\n%s", want, help)
		}
	}

	read := buildGeneratedCommand(accountOp(t, "public-api.v1.account.members.list"))
	if !strings.HasPrefix(read.Use, "list [account]") {
		t.Errorf("Use = %q: el argumento de cuenta va primero y opcional", read.Use)
	}
	write := buildGeneratedCommand(accountOp(t, "public-api.v1.account.members.create"))
	if !strings.HasPrefix(write.Use, "create [account]") || !strings.Contains(write.Long, "Es una escritura") {
		t.Errorf("una escritura con slot explica su regla; Use = %q\n%s", write.Use, write.Long)
	}
	if !strings.Contains(read.Long, "Es una lectura") {
		t.Errorf("una lectura explica que la cuenta se deduce:\n%s", read.Long)
	}
}

// TestSplitPositionalsKeepsTheCompanySignature fija con salidas LITERALES el
// comportamiento de empresa de `splitPositionals`, la firma previa al eje de
// cuenta.
func TestSplitPositionalsKeepsTheCompanySignature(t *testing.T) {
	op := frozenInvoicesShow()
	for _, tc := range []struct {
		name    string
		args    []string
		company string
		want    []string
	}{
		{"empresa posicional", []string{frozenCompanyID, frozenInvoiceID}, "", []string{frozenCompanyID, frozenInvoiceID}},
		{"empresa por flag", []string{frozenInvoiceID}, frozenCompanyID, []string{frozenCompanyID, frozenInvoiceID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gotPath, gotExtra, err := op.splitPositionals(tc.args, tc.company)
			if err != nil || !reflect.DeepEqual(gotPath, tc.want) || len(gotExtra) != 0 {
				t.Fatalf("splitPositionals(%v, %q) = %v, %v, %v; want %v sin sobrante", tc.args, tc.company, gotPath, gotExtra, err, tc.want)
			}
		})
	}

	t.Run("posicional sobrante tras la ruta", func(t *testing.T) {
		gotPath, gotExtra, err := op.splitPositionals([]string{frozenCompanyID, frozenInvoiceID, "campo"}, "")
		if err != nil || !reflect.DeepEqual(gotPath, []string{frozenCompanyID, frozenInvoiceID}) || !reflect.DeepEqual(gotExtra, []string{"campo"}) {
			t.Fatalf("= %v, %v, %v", gotPath, gotExtra, err)
		}
	})

	t.Run("sin empresa por ninguna vía", func(t *testing.T) {
		_, _, err := op.splitPositionals([]string{frozenInvoiceID}, "")
		requireUsageError(t, err, "una operación de empresa sin empresa")
		const want = "falta la empresa: pásala como argumento posicional (<company>) o fíjala con la flag persistente --company"
		if err.Error() != want {
			t.Fatalf("mensaje = %q, want %q", err.Error(), want)
		}
	})

	t.Run("empresa por las dos vías", func(t *testing.T) {
		_, _, err := op.splitPositionals([]string{frozenCompanyID, frozenInvoiceID}, frozenCompanyID)
		requireUsageError(t, err, "la empresa por posicional y por flag")
		want := "has aportado la empresa por las dos vías: como argumento posicional (" + `"` + frozenCompanyID + `"` +
			") y con la flag persistente --company (" + `"` + frozenCompanyID + `"` + "); quita una de las dos, no elijo por ti"
		if err.Error() != want {
			t.Fatalf("mensaje = %q, want %q", err.Error(), want)
		}
	})

	t.Run("el valor de cuenta no rellena un slot de empresa", func(t *testing.T) {
		_, _, err := op.splitAxisPositionals([]string{frozenInvoiceID}, axisValues{Account: flagAcc})
		if err == nil {
			t.Fatal("el valor de cuenta no debe rellenar un slot de empresa")
		}
	})
}

// TestAccountTypedBodyWriteWithoutAccountSendsNothing: una escritura con
// CUERPO TIPADO (`--url`, `--enabled-events`) sin cuenta ni posicional es un
// error de uso y no llega ningún POST; solo se lee `GET /v1/me` para nombrar la
// cuenta propia en el mensaje.
func TestAccountTypedBodyWriteWithoutAccountSendsNothing(t *testing.T) {
	srv, rec := accountServer(t, identityWithAccount)
	_, err := runAccountRoot(t, srv.URL, "account", "webhooks", "create",
		"--url", "https://example.com/hooks", "--enabled-events", "account.member_added", "--skip-scope-check")
	requireUsageError(t, err, "una escritura con cuerpo tipado sin cuenta")
	for _, want := range []string{ownAccountID, "--account", "<account>"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("el mensaje debe contener %q; got:\n%v", want, err)
		}
	}
	for _, req := range rec.all() {
		if req != "GET "+identityPath() {
			t.Fatalf("solo podía llegar la lectura de identidad, llegó %q (todas: %v)", req, rec.all())
		}
	}
}

// TestAccountSlotConfirmationNamesTheResourceNotTheAccount: borrar un miembro
// confirma el miembro, no la cuenta.
func TestAccountSlotConfirmationNamesTheResourceNotTheAccount(t *testing.T) {
	op := accountOp(t, "public-api.v1.account.members.delete")
	const member = "01931b3e-7c4a-7f2e-9a8b-3c5d6e7f8c07"
	pathValues, _, err := op.splitAxisPositionals([]string{member}, axisValues{Account: flagAcc})
	if err != nil {
		t.Fatalf("reparto: %v", err)
	}
	if len(pathValues) != 2 || pathValues[0] != flagAcc || pathValues[1] != member {
		t.Fatalf("lista normalizada = %v, want [%s %s]", pathValues, flagAcc, member)
	}
	if got := opResourceID(op, pathValues); got != member {
		t.Fatalf("recurso confirmado = %q, want %q (el miembro, no la cuenta)", got, member)
	}
	if want := "/v1/accounts/" + flagAcc + "/members/" + member; op.buildPath(pathValues) != want {
		t.Fatalf("path = %q, want %q", op.buildPath(pathValues), want)
	}
}
