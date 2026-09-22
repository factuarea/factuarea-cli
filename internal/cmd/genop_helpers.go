package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/spf13/cobra"
)

func validateRawJSONBody(body []byte, requireObject bool) ([]byte, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		if requireObject {
			return nil, apierr.Usagef("--data debe ser un objeto JSON (no un array ni un valor escalar)")
		}
		return body, nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, apierr.Usagef("JSON inválido en --data: %v", err)
	}
	if dec.More() {
		return nil, apierr.Usagef("JSON inválido en --data: contenido extra tras el valor JSON")
	}
	if requireObject {
		if _, ok := v.(map[string]any); !ok {
			return nil, apierr.Usagef("--data debe ser un objeto JSON (no un array ni un valor escalar)")
		}
	}
	canonical, err := marshalNoEscape(v, false)
	if err != nil {
		return nil, apierr.Usagef("JSON inválido en --data: %v", err)
	}
	return canonical, nil
}

func bytesTrim(b []byte) []byte {
	return bytes.TrimSpace(b)
}

// companyPathParam es el nombre del marcador de ruta del eje de empresa.
const companyPathParam = "company"

// companyAxisPrefix es el prefijo de path del eje de empresa del contrato v1.
const companyAxisPrefix = "/companies/{" + companyPathParam + "}"

// companyResourceGroup es el grupo de comandos que gestiona la empresa como
// RECURSO (`factuarea companies …`).
const companyResourceGroup = "companies"

// isCompanyAxisPath decide si la operación cuelga del EJE de empresa, y lo
// decide por el PATH, nunca por el nombre del marcador: `DELETE
// /accounts/{account}/companies/{company}` declara un `{company}` que no es el
// eje sino el RECURSO, la empresa gestionada que se archiva dentro de una
// cartera. Clasificarlo por nombre rellenaba ese destino con la empresa de la
// credencial.
func (op genOp) isCompanyAxisPath() bool {
	rest, ok := strings.CutPrefix(op.Path, companyAxisPrefix)
	return ok && (rest == "" || strings.HasPrefix(rest, "/"))
}

// companyPathParamIndex devuelve la posición del slot de EJE de empresa
// dentro de los parámetros de ruta, o -1 si la operación no cuelga del eje.
func (op genOp) companyPathParamIndex() int {
	if !op.isCompanyAxisPath() {
		return -1
	}
	if len(op.PathParams) == 0 || op.PathParams[0].Name != companyPathParam {
		return -1
	}
	return 0
}

// requiresExplicitCompany es la SALVAGUARDA del eje: en una irreversible cuyo
// último parámetro de ruta es el propio eje, el valor se escribe. Confirmar
// sobre un identificador que la herramienta acaba de inventar no confirma nada.
func (op genOp) requiresExplicitCompany() bool {
	idx := op.companyPathParamIndex()
	return op.Irreversible && idx >= 0 && idx == len(op.PathParams)-1
}

// rejectsAutomaticCompany cubre lo que el contrato NO marca: una mutación cuyo
// recurso ES la empresa no admite que el eje lo aporte el eslabón automático.
// El posicional y `--company` sí valen; lo que no puede ocurrir es que una
// invocación sin un solo dato archive o desactive la empresa de la credencial,
// que además no pasa por `safety.Confirm` al no llevar la marca.
func (op genOp) rejectsAutomaticCompany() bool {
	idx := op.companyPathParamIndex()
	if idx < 0 || idx != len(op.PathParams)-1 {
		return false
	}
	return op.isMutating() && op.actsOnTheCompanyItself()
}

// actsOnTheCompanyItself separa la operación que cambia la EMPRESA de la que
// crea algo DENTRO de ella. Lo decide el grupo de comandos, no el path: `POST
// /companies/{company}/invoices` cuelga del eje igual, pero su recurso es la
// factura y ahí el relleno por defecto es justo lo que se quiere.
func (op genOp) actsOnTheCompanyItself() bool {
	return len(op.Groups) > 0 && op.Groups[len(op.Groups)-1] == companyResourceGroup
}

// companyFillIndex devuelve la posición del slot de eje que `--company` —o la
// resolución automática de `resolveCompany`— puede rellenar, o -1 si esta
// operación no admite relleno por defecto.
func (op genOp) companyFillIndex() int {
	if op.requiresExplicitCompany() {
		return -1
	}
	return op.companyPathParamIndex()
}

// explicitCompanyError es el fallo ÚNICO de la salvaguarda, para que la
// aridad y el reparto de posicionales digan exactamente lo mismo.
func (op genOp) explicitCompanyError() error {
	return apierr.Usagef(
		"esta operación es irreversible y actúa sobre la empresa ENTERA: escribe su identificador como argumento posicional (<%s>). La flag persistente --company no lo rellena por defecto a propósito, porque confirmar sobre un identificador que no has escrito no confirma nada",
		companyPathParam)
}

// automaticCompanyError es el fallo de `rejectsAutomaticCompany`: nombra las
// dos vías que sí valen y dice por qué no se deduce la tercera.
func (op genOp) automaticCompanyError() error {
	return apierr.Usagef(
		"esta operación actúa sobre la empresa ENTERA y no pide confirmación: escríbela como argumento posicional (<%s>) o fíjala con la flag persistente --company. No la deduzco del ámbito de la credencial: un cambio así no puede ir a parar a una empresa que no hayas nombrado",
		companyPathParam)
}

// splitPositionals reparte los argumentos posicionales de una invocación
// entre los parámetros de RUTA (devueltos ya en el orden del contrato y con
// el valor de empresa resuelto) y el resto, que es el atajo del campo único
// de cuerpo.
func (op genOp) splitPositionals(args []string, company string) (pathValues, extra []string, err error) {
	n := len(op.PathParams)
	idx := op.companyFillIndex()
	if idx < 0 {
		if len(args) < n {
			if op.requiresExplicitCompany() {
				return nil, nil, op.explicitCompanyError()
			}
			return nil, nil, apierr.Usagef("faltan argumentos: esta operación necesita %d (%s)", n, strings.Join(pathParamNames(op), ", "))
		}
		return args[:n], args[n:], nil
	}

	resolved := strings.TrimSpace(company)
	switch {
	case resolved != "" && len(args) >= n:
		return nil, nil, apierr.Usagef(
			"has aportado la empresa por las dos vías: como argumento posicional (%q) y con la flag persistente --company (%q); quita una de las dos, no elijo por ti",
			args[idx], resolved)
	case resolved == "" && len(args) < n:
		return nil, nil, apierr.Usagef(
			"falta la empresa: pásala como argumento posicional (<%s>) o fíjala con la flag persistente --company",
			op.PathParams[idx].Name)
	case resolved == "":
		return args[:n], args[n:], nil
	}

	pathValues = make([]string, 0, n)
	rest := args
	for i, p := range op.PathParams {
		if i == idx {
			pathValues = append(pathValues, resolved)
			continue
		}
		if len(rest) == 0 {
			return nil, nil, apierr.Usagef("falta el id del recurso (%s)", p.Name)
		}
		pathValues = append(pathValues, rest[0])
		rest = rest[1:]
	}
	return pathValues, rest, nil
}

// extraPositionals devuelve los posicionales que no ocupa ningún parámetro de
// ruta, sin exigir el eje.
func (op genOp) extraPositionals(args []string) []string {
	n := len(op.PathParams)
	if op.companyFillIndex() >= 0 {
		n--
	}
	if n < 0 || len(args) <= n {
		return nil
	}
	return args[n:]
}

func pathParamNames(op genOp) []string {
	names := make([]string, 0, len(op.PathParams))
	for _, p := range op.PathParams {
		names = append(names, p.Name)
	}
	return names
}

// opResourceID devuelve el identificador del RECURSO sobre el que actúa la
// operación, que es el último parámetro de ruta que NO es el slot de EJE.
func opResourceID(op genOp, pathValues []string) string {
	if len(op.PathParams) == 0 || len(pathValues) != len(op.PathParams) {
		return ""
	}
	idx := op.companyPathParamIndex()
	for i := len(pathValues) - 1; i >= 0; i-- {
		if i == idx {
			continue
		}
		return pathValues[i]
	}
	// La operación sólo declara el eje: entonces la empresa ES el recurso
	// —`POST /companies/{company}/invoices/bulk-delete` borra las facturas de
	// esa empresa entera— y nombrarla en la confirmación es exactamente lo que
	// hay que hacer.
	return pathValues[idx]
}

func writeDeleteConfirmation(cmd *cobra.Command, op genOp, pathValues []string, format output.Format) error {
	id := opResourceID(op, pathValues)
	if format == output.JSON {
		payload := map[string]any{"deleted": true}
		if id != "" {
			payload["id"] = id
		}
		return output.PrintJSON(cmd.OutOrStdout(), payload)
	}
	if id != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Eliminado (%s).\n", id)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "✓ Eliminado.")
	}
	return nil
}

func writeMutationConfirmation(cmd *cobra.Command, op genOp, pathValues []string, format output.Format) error {
	id := opResourceID(op, pathValues)
	if format == output.JSON {
		payload := map[string]any{"ok": true}
		if id != "" {
			payload["id"] = id
		}
		return output.PrintJSON(cmd.OutOrStdout(), payload)
	}
	if id != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ Operación completada (%s).\n", id)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "✓ Operación completada.")
	}
	return nil
}

func validateResourceArgs(op genOp, pathValues []string) error {
	for i := range op.PathParams {
		if i < len(pathValues) && strings.TrimSpace(pathValues[i]) == "" {
			return apierr.Usagef("falta el id del recurso (%s)", op.PathParams[i].Name)
		}
	}
	return nil
}

func (op genOp) isMutating() bool {
	if scope := op.RequiredScope; scope != "" {
		return !strings.HasSuffix(scope, ":read")
	}
	switch op.Method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

func (op genOp) isPaginated() bool {
	for _, p := range op.QueryParams {
		if p.Name == "starting_after" {
			return true
		}
	}
	return false
}

func (op genOp) confirmResourceID(pathValues []string) string {
	if id := opResourceID(op, pathValues); id != "" {
		return id
	}
	return op.Action
}

// buildPath compone el path de la petición con los valores de ruta YA
// resueltos por `splitPositionals`, de modo que da igual si la empresa llegó
// por el posicional o por la flag persistente.
func (op genOp) buildPath(pathValues []string) string {
	path := op.Path
	for i, p := range op.PathParams {
		if i >= len(pathValues) {
			break
		}
		path = strings.Replace(path, "{"+p.Name+"}", url.PathEscape(pathValues[i]), 1)
	}
	if !strings.HasPrefix(path, "/v1") {
		path = "/v1" + path
	}
	return path
}

func (op genOp) buildBody(data, dataFile string, files map[string]*string) ([]byte, map[string]string, error) {
	if op.Body == nil {
		return nil, nil, nil
	}
	if op.Body.Kind == "json" {
		if dataFile != "" {
			b, err := readInputFile(dataFile)
			return b, nil, err
		}
		if data != "" {
			return []byte(data), nil, nil
		}
		return nil, nil, nil
	}
	fileMap := map[string]string{}
	for field, v := range files {
		if v != nil && *v != "" {
			fileMap[field] = *v
		}
	}
	if len(fileMap) == 0 {
		return nil, nil, apierr.Usagef("falta --file-<campo> para el upload (%s)", strings.Join(op.Body.FileFields, ", "))
	}
	fields := map[string]string{}
	if data != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			return nil, nil, fmt.Errorf("--data debe ser un objeto JSON plano de campos de texto: %w", err)
		}
		for k, v := range m {
			fields[k] = fmt.Sprint(v)
		}
	}
	body, ct, err := client.MultipartBody(fields, fileMap)
	if err != nil {
		return nil, nil, err
	}
	return body, map[string]string{"Content-Type": ct}, nil
}
