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

// companyPathParam es el nombre del parámetro de ruta que el contrato v1 usa
// para el eje de empresa (`/companies/{company}/<recurso>`). Es el ÚNICO
// parámetro que la flag persistente `--company` puede rellenar.
const companyPathParam = "company"

// companyPathParamIndex devuelve la posición del parámetro de empresa dentro de
// los parámetros de ruta de la operación, o -1 si la operación no lo declara.
func (op genOp) companyPathParamIndex() int {
	for i, p := range op.PathParams {
		if p.Name == companyPathParam {
			return i
		}
	}
	return -1
}

// splitPositionals reparte los argumentos posicionales de una invocación entre
// los parámetros de RUTA (devueltos ya en el orden del contrato y con el valor
// de empresa resuelto) y el resto, que es el atajo del campo único de cuerpo.
//
// El eje de empresa es ADITIVO: cuando la operación declara `{company}`, el
// valor puede venir del posicional (comportamiento de siempre) o de la flag
// persistente `--company`, pero NUNCA de las dos vías a la vez ni de ninguna.
func (op genOp) splitPositionals(args []string, companyFlag string) (pathValues, extra []string, err error) {
	n := len(op.PathParams)
	idx := op.companyPathParamIndex()
	if idx < 0 {
		if len(args) < n {
			return nil, nil, apierr.Usagef("faltan argumentos: esta operación necesita %d (%s)", n, strings.Join(pathParamNames(op), ", "))
		}
		return args[:n], args[n:], nil
	}

	company := strings.TrimSpace(companyFlag)
	switch {
	case company != "" && len(args) >= n:
		return nil, nil, apierr.Usagef(
			"has aportado la empresa por las dos vías: como argumento posicional (%q) y con la flag persistente --company (%q); quita una de las dos, no elijo por ti",
			args[idx], company)
	case company == "" && len(args) < n:
		return nil, nil, apierr.Usagef(
			"falta la empresa: pásala como argumento posicional (<%s>) o fíjala con la flag persistente --company",
			op.PathParams[idx].Name)
	case company == "":
		return args[:n], args[n:], nil
	}

	pathValues = make([]string, 0, n)
	rest := args
	for i, p := range op.PathParams {
		if i == idx {
			pathValues = append(pathValues, company)
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

func pathParamNames(op genOp) []string {
	names := make([]string, 0, len(op.PathParams))
	for _, p := range op.PathParams {
		names = append(names, p.Name)
	}
	return names
}

// opResourceID devuelve el identificador del RECURSO sobre el que actúa la
// operación, que es el último parámetro de ruta que NO es la empresa.
//
// Antes del eje de empresa bastaba con «el último posicional» y acertaba por
// casualidad. Con `--company` ese «último» cambia de sitio: en
// `companies api-keys revoke --company acme key_7` el último posicional es la
// clave —correcto— pero en `companies delete --company acme` no hay ninguno, y
// en una operación cuyo `{company}` fuera el ÚLTIMO segmento la confirmación
// habría nombrado a la empresa en vez de al recurso. Se calcula sobre los
// valores YA resueltos y se excluye explícitamente el eje.
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
	// (`DELETE /companies/{company}`), y nombrarla en la confirmación es
	// exactamente lo que hay que hacer.
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

// buildPath compone el path de la petición con los valores de ruta YA resueltos
// por `splitPositionals`, de modo que da igual si la empresa llegó por el
// posicional o por la flag persistente: aquí siempre hay un valor por parámetro.
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
