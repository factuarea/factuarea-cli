package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/spf13/cobra"
)

func validateCRMResourceArgs(op genOp, args []string) error {
	if op.CrmOperation == "" {
		return nil
	}
	for i, p := range op.PathParams {
		if i >= len(args) {
			return apierr.Usagef("falta la identidad %s", p.Name)
		}
		if p.Format != "uuid" {
			continue
		}
		pattern := p.Pattern
		if pattern == "" {
			pattern = `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-7[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return apierr.Usagef("el contrato UUID CRM de %s no es compatible", p.Name)
		}
		if !compiled.MatchString(args[i]) {
			return apierr.Usagef("%s debe ser el UUID público de su recurso CRM", p.Name)
		}
	}
	return nil
}

func runCRMPaginated(cmd *cobra.Command, cc *cliContext, path string, queryValues url.Values, op genOp, maxPages int) error {
	p := op.Pagination
	if p == nil {
		return apierr.Usagef("la operación no declara paginación CRM completa")
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	err := cc.client.PaginateCRM(cmd.Context(), path, queryValues, client.CRMPagination{Kind: p.Kind, QueryParameter: p.QueryParameter, ItemsPath: p.ItemsPath, MorePath: p.MorePath, CursorPath: p.CursorPath, CurrentPagePath: p.CurrentPagePath, LastPagePath: p.LastPagePath, IdentityPath: p.IdentityPath, MaxPages: maxPages}, func(item json.RawMessage) error { return enc.Encode(item) })
	if err != nil {
		output.PrintError(cmd.ErrOrStderr(), err, cc.errorFormat)
		return &AlreadyReported{Err: err}
	}
	return nil
}

// Every current native CRM success declares a data envelope. An empty or
// malformed write response cannot be promoted to a fabricated confirmation.
func validateCRMResponse(op genOp, body []byte) error {
	if op.CrmOperation == "" {
		return nil
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(body, &envelope) == nil && envelope != nil {
		if data, ok := envelope["data"]; ok && string(data) != "null" {
			return nil
		}
	}
	if op.isMutating() {
		return &apierr.TransportError{Err: fmt.Errorf("la respuesta CRM no confirma el resultado original")}
	}
	return &apierr.APIError{Type: "api_error", Code: "invalid_crm_response", Message: "La respuesta CRM no contiene el envelope nativo publicado."}
}

func printCRMMutationError(cmd *cobra.Command, cc *cliContext, op genOp, key string, err error) {
	var transport *apierr.TransportError
	if op.CrmOperation != "" && op.isMutating() && errors.As(err, &transport) {
		// A local unconfirmed transport result is not a server HTTP conflict or
		// an original receipt. Preserve the real error for the semantic exit code.
		output.PrintError(cmd.ErrOrStderr(), &apierr.APIError{Type: "transport_error", Code: "cli_mutation_unconfirmed", Message: fmt.Sprintf("No se ha confirmado el resultado de la intención original. Conserva --idempotency-key %q y consulta el estado antes de repetir; el CLI no reenvía esta escritura automáticamente.", key), Details: map[string]any{"idempotency_key": key, "operation_id": op.OperationID, "state": "unconfirmed"}}, cc.errorFormat)
		return
	}
	output.PrintError(cmd.ErrOrStderr(), err, cc.errorFormat)
}
