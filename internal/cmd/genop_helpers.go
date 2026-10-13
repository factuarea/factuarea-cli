package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

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
	dec.UseNumber()
	if !utf8.Valid(body) {
		return nil, apierr.Usagef("--data debe conservar UTF-8 válido")
	}
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, apierr.Usagef("JSON inválido en --data: %v", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, apierr.Usagef("JSON inválido en --data: contenido extra tras el valor JSON")
	}
	if requireObject {
		if _, ok := v.(map[string]any); !ok {
			return nil, apierr.Usagef("--data debe ser un objeto JSON (no un array ni un valor escalar)")
		}
	}
	// Validation does not rewrite whitespace, decimals, nulls or JSON key order.
	// Recovery hashes these exact bytes, the same bytes sent by Client.Do.
	return body, nil
}

func bytesTrim(b []byte) []byte {
	return bytes.TrimSpace(b)
}

func opResourceID(op genOp, args []string) string {
	if len(op.PathParams) > 0 && len(args) >= len(op.PathParams) {
		return args[len(op.PathParams)-1]
	}
	return ""
}

func writeDeleteConfirmation(cmd *cobra.Command, op genOp, args []string, format output.Format) error {
	id := opResourceID(op, args)
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

func writeMutationConfirmation(cmd *cobra.Command, op genOp, args []string, format output.Format) error {
	id := opResourceID(op, args)
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

func validateResourceArgs(op genOp, args []string) error {
	for i := range op.PathParams {
		if i < len(args) && strings.TrimSpace(args[i]) == "" {
			return apierr.Usagef("falta el id del recurso (%s)", op.PathParams[i].Name)
		}
	}
	if isCRMCoreOperation(op) {
		for i := range op.PathParams {
			if i < len(args) && !regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(args[i]) {
				return apierr.Usagef("%s debe conservar un UUID v7 público", op.PathParams[i].Name)
			}
		}
	}
	return nil
}

func (op genOp) isMutating() bool {
	// Original receipt and preparation GETs require current writer authority.
	// That scope does not turn their HTTP read into a mutation or a live write.
	if op.Method == "GET" || op.Method == "HEAD" || op.Method == "OPTIONS" {
		return false
	}
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

func (op genOp) confirmResourceID(args []string) string {
	if len(op.PathParams) > 0 && len(args) >= len(op.PathParams) {
		return args[len(op.PathParams)-1]
	}
	return op.Action
}

func (op genOp) buildPath(args []string) string {
	path := op.Path
	for i, p := range op.PathParams {
		path = strings.Replace(path, "{"+p.Name+"}", url.PathEscape(args[i]), 1)
	}
	if !strings.HasPrefix(path, "/v1") {
		path = "/v1" + path
	}
	return path
}

func (op genOp) buildBody(data, dataFile string, files map[string]*string, fileArrays map[string]*[]string) ([]byte, map[string]string, error) {
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
	arrayMap := map[string][]string{}
	for field, paths := range fileArrays {
		if paths != nil && len(*paths) > 0 {
			arrayMap[field+"[]"] = *paths
		}
	}
	if len(fileMap) == 0 && len(arrayMap) == 0 {
		return nil, nil, apierr.Usagef("falta --file-<campo> para el upload (%s)", strings.Join(append(append([]string{}, op.Body.FileFields...), op.Body.FileArrayFields...), ", "))
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
	body, ct, err := client.MultipartBody(fields, fileMap, arrayMap)
	if err != nil {
		return nil, nil, err
	}
	return body, map[string]string{"Content-Type": ct}, nil
}

// Preserve a separately declared scalar parameter instead of shadowing its flag
// with the convenience alias of the corresponding array parameter.
func (op genOp) arrayQueryFlag(name string) string {
	alias := strings.TrimSuffix(name, "[]")
	for _, parameter := range op.QueryParams {
		if parameter.Name == alias {
			return name
		}
	}
	return alias
}
