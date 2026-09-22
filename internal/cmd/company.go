package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

// Resolución del EJE DE EMPRESA del contrato v1. CADENA DE PRECEDENCIA, de
// mayor a menor:
//
//  1. Argumento POSICIONAL en la posición del slot de eje.
//  2. Flag persistente `--company`.
//  3. Resolución AUTOMÁTICA si el ámbito de la credencial alcanza un solo NIF.
//  4. Error accionable que ENUMERA los identificadores del ámbito.
//
// Son CUATRO eslabones: no hay variable de entorno ni clave de `config.toml`,
// porque ninguna de las dos existe en esta CLI. Nada de esto escribe en disco.

// identityOperationID es el identificador ESTABLE de la operación de
// identidad de la credencial.
const identityOperationID = "public-api.v1.account.show"

// identityFallbackPath es el path que declara el documento del eje de
// empresa.
const identityFallbackPath = "/v1/me"

// identityPath compone el path de la operación de identidad tal como lo
// declara el contrato embebido.
func identityPath() string {
	for _, op := range generatedOps() {
		if op.OperationID == identityOperationID && len(op.PathParams) == 0 {
			return op.buildPath(nil)
		}
	}
	return identityFallbackPath
}

// companyBasePath compone el prefijo del EJE DE EMPRESA para las superficies
// ESCRITAS A MANO —el devloop (`listen`, `trigger`)—, que no salen del árbol
// generado y por tanto no heredan el path del contrato. Desde el eje NINGÚN
// recurso de empresa cuelga de una ruta plana, así que un `/v1/<recurso>`
// escrito a mano es un 404 garantizado contra la v1.
func companyBasePath(company string) string {
	return "/v1/companies/" + url.PathEscape(company)
}

// companyScopeEntry es una entrada del ámbito de la credencial.
type companyScopeEntry struct {
	ID    string `json:"id"`
	TaxID string `json:"tax_id"`
	Name  string `json:"name"`
}

// companyResolution memoiza la resolución automática POR PROCESO.
var companyResolution struct {
	mu      sync.Mutex
	key     string
	value   string
	err     error
	resolvd bool
}

// resetCompanyResolution vacía la memoria del proceso. Existe para los tests,
// que ejercitan varias credenciales y varios ámbitos dentro del mismo binario
// de pruebas; en producción el proceso muere con el comando.
func resetCompanyResolution() {
	companyResolution.mu.Lock()
	defer companyResolution.mu.Unlock()
	companyResolution.key = ""
	companyResolution.value = ""
	companyResolution.err = nil
	companyResolution.resolvd = false
}

// resolveCompany devuelve el valor de empresa que `splitPositionals` debe
// usar, aplicando la cadena de precedencia.
func resolveCompany(ctx context.Context, g *GlobalFlags, positional string) (string, error) {
	flag := strings.TrimSpace(g.Company)
	if strings.TrimSpace(positional) != "" {
		return flag, nil
	}
	if flag != "" {
		return flag, nil
	}
	return autoResolveCompany(ctx, g)
}

// autoResolveCompany es el eslabón 3: lee el ámbito de la credencial y, si
// alcanza un solo NIF, lo usa. Es el ÚNICO eslabón que toca la red.
func autoResolveCompany(ctx context.Context, g *GlobalFlags) (string, error) {
	cc, err := newCLIContext(g, "")
	if err != nil {
		// Sin credencial resuelta no hay ámbito que leer.
		return "", missingCompanyError()
	}

	key := cc.res.APIKey + "\x00" + os.Getenv(envBaseURL)
	companyResolution.mu.Lock()
	defer companyResolution.mu.Unlock()
	if companyResolution.resolvd && companyResolution.key == key {
		return companyResolution.value, companyResolution.err
	}

	value, rerr := readSingleCompanyScope(ctx, cc)
	companyResolution.key = key
	companyResolution.value = value
	companyResolution.err = rerr
	companyResolution.resolvd = true
	return value, rerr
}

func readSingleCompanyScope(ctx context.Context, cc *cliContext) (string, error) {
	resp, err := cc.client.Do(ctx, "GET", identityPath(), nil, nil)
	if err != nil {
		return "", apierr.Usagef(
			"no he podido leer el ámbito de la credencial para deducir la empresa (%v): pásala como argumento posicional (<%s>) o fíjala con la flag persistente --company",
			err, companyPathParam)
	}
	var payload struct {
		Data struct {
			Scope []companyScopeEntry `json:"scope"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return "", apierr.Usagef(
			"no he podido interpretar el ámbito de la credencial para deducir la empresa (%v): pásala como argumento posicional (<%s>) o fíjala con la flag persistente --company",
			err, companyPathParam)
	}
	scope := payload.Data.Scope
	switch {
	case len(scope) == 1 && strings.TrimSpace(scope[0].ID) != "":
		return scope[0].ID, nil
	case len(scope) > 1:
		return "", ambiguousCompanyError(scope)
	default:
		return "", missingCompanyError()
	}
}

// missingCompanyError es el fallo cuando no hay empresa por ninguna vía y el
// ámbito no permite deducirla.
func missingCompanyError() error {
	return apierr.Usagef(
		"falta la empresa: pásala como argumento posicional (<%s>) o fíjala con la flag persistente --company",
		companyPathParam)
}

// ambiguousCompanyError es el eslabón 4. ENUMERA el ámbito en vez de
// limitarse a pedir la flag.
func ambiguousCompanyError(scope []companyScopeEntry) error {
	var b strings.Builder
	fmt.Fprintf(&b, "tu credencial alcanza %d empresas y no elijo por ti; pasa una como argumento posicional (<%s>) o fíjala con --company:",
		len(scope), companyPathParam)
	for _, e := range scope {
		b.WriteString("\n  --company " + e.ID)
		switch {
		case e.TaxID != "" && e.Name != "":
			b.WriteString("   " + e.TaxID + " — " + e.Name)
		case e.TaxID != "":
			b.WriteString("   " + e.TaxID)
		case e.Name != "":
			b.WriteString("   " + e.Name)
		}
	}
	return apierr.Usagef("%s", b.String())
}

// resolveCompanyForArgs aplica la cadena a UNA invocación concreta.
func (op genOp) resolveCompanyForArgs(ctx context.Context, g *GlobalFlags, args []string) (string, error) {
	idx := op.companyFillIndex()
	if idx < 0 {
		return g.Company, nil
	}
	positional := ""
	if len(args) >= len(op.PathParams) && idx < len(args) {
		positional = args[idx]
	}
	// El eslabón 3 se corta en las mutaciones cuyo recurso ES la empresa: ahí el
	// valor tiene que haberlo escrito el usuario, por una de las dos vías.
	if op.rejectsAutomaticCompany() && strings.TrimSpace(positional) == "" && strings.TrimSpace(g.Company) == "" {
		return "", op.automaticCompanyError()
	}
	return resolveCompany(ctx, g, positional)
}
