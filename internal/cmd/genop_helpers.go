package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/client"
)

// isMutating indica si la operación cambia estado (aplica el guard --live).
func (op genOp) isMutating() bool {
	switch op.Method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

// isPaginated indica si el listado soporta paginación cursor (--paginate).
func (op genOp) isPaginated() bool {
	for _, p := range op.QueryParams {
		if p.Name == "starting_after" {
			return true
		}
	}
	return false
}

// buildPath sustituye cada {param} por su argumento posicional (en orden) y
// antepone /v1 si la ruta no lo trae ya.
func (op genOp) buildPath(args []string) string {
	path := op.Path
	for i, p := range op.PathParams {
		path = strings.Replace(path, "{"+p.Name+"}", args[i], 1)
	}
	if !strings.HasPrefix(path, "/v1") {
		path = "/v1" + path
	}
	return path
}

// buildBody devuelve el cuerpo y los headers según el tipo de body.
//   - sin body: nil, nil, nil.
//   - json: lee --data-file (si se pasó) o --data; vacío si ninguno.
//   - multipart: exige al menos un --file-<campo>; mete los campos de texto de
//     --data como JSON plano {campo:valor} y delega en client.MultipartBody.
func (op genOp) buildBody(data, dataFile string, files map[string]*string) ([]byte, map[string]string, error) {
	if op.Body == nil {
		return nil, nil, nil
	}
	if op.Body.Kind == "json" {
		if dataFile != "" {
			b, err := os.ReadFile(dataFile)
			return b, nil, err
		}
		if data != "" {
			return []byte(data), nil, nil
		}
		return nil, nil, nil // body opcional vacío; la API valida
	}
	// multipart
	fileMap := map[string]string{}
	for field, v := range files {
		if v != nil && *v != "" {
			fileMap[field] = *v
		}
	}
	if len(fileMap) == 0 {
		return nil, nil, fmt.Errorf("falta --file-<campo> para el upload (%s)", strings.Join(op.Body.FileFields, ", "))
	}
	// Los campos de texto extra del multipart van por --data como JSON plano {campo:valor}.
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
