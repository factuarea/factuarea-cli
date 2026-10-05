package cmd

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"

	"github.com/factuarea/factuarea-cli/internal/apierr"
)

// Resolución del EJE DE CUENTA del contrato v1. CADENA DE PRECEDENCIA, de
// mayor a menor:
//
//  1. Argumento POSICIONAL en la posición del slot de eje.
//  2. Flag persistente `--account`.
//  3. Resolución AUTOMÁTICA desde la identidad de la credencial
//     (`data.account.id` de la operación de identidad), SOLO en las lecturas.
//
// En una escritura sin cuenta por las dos primeras vías, la CLI termina con
// error de uso SIN enviar la escritura: una escritura no puede ir a parar a una
// cuenta que el usuario no ha nombrado. Para nombrar la cuenta propia en el
// mensaje hace un único `GET` de identidad, cuyo resultado no usa como destino.
//
// En el grupo `managed-accounts` el `{account}` es la cuenta GESTIONADA, un
// RECURSO: ni flag ni relleno (`accountFillIndex` devuelve -1). Las rutas raíz
// de provisión e importación no tienen slot. Nada de esto escribe en disco.

// accountResolution memoiza la cuenta propia POR PROCESO, igual que
// `companyResolution`, indexada por la credencial y la URL base.
var accountResolution struct {
	mu      sync.Mutex
	key     string
	value   string
	err     error
	resolvd bool
}

// resetAccountResolution vacía la memoria del proceso. Existe para los tests,
// que ejercitan varias credenciales dentro del mismo binario de pruebas; en
// producción el proceso muere con el comando.
func resetAccountResolution() {
	accountResolution.mu.Lock()
	defer accountResolution.mu.Unlock()
	accountResolution.key = ""
	accountResolution.value = ""
	accountResolution.err = nil
	accountResolution.resolvd = false
}

// resolveAccount devuelve el valor de cuenta que `splitAxisPositionals` debe
// usar, aplicando la cadena de precedencia. `allowAuto` es verdadero solo en
// las lecturas; con él falso y sin cuenta por ninguna de las dos primeras vías,
// devuelve el error de escritura que nombra la cuenta propia.
func resolveAccount(ctx context.Context, g *GlobalFlags, positional string, allowAuto bool) (string, error) {
	flag := strings.TrimSpace(g.Account)
	if strings.TrimSpace(positional) != "" {
		return flag, nil
	}
	if flag != "" {
		return flag, nil
	}
	own, err := autoResolveAccount(ctx, g)
	if !allowAuto {
		if err != nil || strings.TrimSpace(own) == "" {
			// Sin identidad legible no se puede nombrar la cuenta propia, pero la
			// escritura se rechaza igual.
			return "", missingAccountError()
		}
		return "", accountWriteNeedsNameError(own)
	}
	return own, err
}

// autoResolveAccount es el eslabón 3: lee la cuenta de la credencial. Es el
// ÚNICO eslabón que toca la red.
func autoResolveAccount(ctx context.Context, g *GlobalFlags) (string, error) {
	cc, err := newCLIContext(g, "")
	if err != nil {
		// Sin credencial resuelta no hay identidad que leer.
		return "", missingAccountError()
	}

	key := cc.res.APIKey + "\x00" + os.Getenv(envBaseURL)
	accountResolution.mu.Lock()
	defer accountResolution.mu.Unlock()
	if accountResolution.resolvd && accountResolution.key == key {
		return accountResolution.value, accountResolution.err
	}

	value, rerr := readOwnAccount(ctx, cc)
	accountResolution.key = key
	accountResolution.value = value
	accountResolution.err = rerr
	accountResolution.resolvd = true
	return value, rerr
}

func readOwnAccount(ctx context.Context, cc *cliContext) (string, error) {
	resp, err := cc.client.Do(ctx, "GET", identityPath(), nil, nil)
	if err != nil {
		return "", apierr.Usagef(
			"no he podido leer la identidad de la credencial para deducir la cuenta (%v): pásala como argumento posicional (<%s>) o fíjala con la flag persistente --account",
			err, accountPathParam)
	}
	var payload struct {
		Data struct {
			Account struct {
				ID string `json:"id"`
			} `json:"account"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return "", apierr.Usagef(
			"no he podido interpretar la identidad de la credencial para deducir la cuenta (%v): pásala como argumento posicional (<%s>) o fíjala con la flag persistente --account",
			err, accountPathParam)
	}
	if id := strings.TrimSpace(payload.Data.Account.ID); id != "" {
		return id, nil
	}
	return "", missingAccountError()
}

// missingAccountError es el fallo cuando no hay cuenta por ninguna vía y no se
// puede deducir.
func missingAccountError() error {
	return apierr.Usagef(
		"falta la cuenta: pásala como argumento posicional (<%s>) o fíjala con la flag persistente --account",
		accountPathParam)
}

// accountWriteNeedsNameError es el fallo de una escritura sin cuenta: nombra la
// cuenta propia de la credencial y las dos formas de pasarla, y dice por qué no
// se deduce.
func accountWriteNeedsNameError(own string) error {
	return apierr.Usagef(
		"esta operación escribe en una cuenta y no la deduzco: tu credencial pertenece a la cuenta %s. Escríbela como argumento posicional (<%s>) o fíjala con la flag persistente --account. Una escritura no puede ir a parar a una cuenta que no hayas nombrado",
		own, accountPathParam)
}

// explicitAccountError es el fallo ÚNICO de la salvaguarda de irreversibles
// cuyo último parámetro de ruta es la cuenta, para que la aridad y el reparto
// de posicionales digan exactamente lo mismo.
func (op genOp) explicitAccountError() error {
	return apierr.Usagef(
		"esta operación es irreversible y nombra la cuenta como último argumento (en una transferencia de NIF es la cuenta que cede o recibe el NIF, no solo la que opera): escríbela como argumento posicional (<%s>). La flag persistente --account no la rellena por defecto a propósito, porque confirmar sobre un identificador que no has escrito no confirma nada",
		accountPathParam)
}

// resolveAccountForArgs aplica la cadena a UNA invocación concreta. Devuelve ""
// sin tocar la red cuando la operación no tiene slot de cuenta rellenable.
func (op genOp) resolveAccountForArgs(ctx context.Context, g *GlobalFlags, args []string) (string, error) {
	idx := op.accountFillIndex()
	if idx < 0 {
		return "", nil
	}
	positional := ""
	if len(args) >= len(op.PathParams) && idx < len(args) {
		positional = args[idx]
	}
	return resolveAccount(ctx, g, positional, op.autoResolvesAccount())
}
