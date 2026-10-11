// Generated from one actual private native artifact, with publication and delivery OFF.
package cmd

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/factuarea/factuarea-cli/internal/spec"
	"github.com/spf13/cobra"
)

func registerCrmNative134Commands(root *cobra.Command) {
	group := func(parent *cobra.Command, name string) *cobra.Command {
		for _, child := range parent.Commands() {
			if child.Name() == name {
				return child
			}
		}
		child := serviceLevelGroup(name, "Controles CRM nativos privados")
		parent.AddCommand(child)
		return child
	}
	crm := group(root, "crm")
	stages := group(crm, "service-stages")
	stages.AddCommand(newCrmNative134("stage_list", "list", ""), newCrmNative134("stage_update", "update", "stage"), newCrmNative134("stage_original", "original", "stage"))
	bulk := group(group(crm, "service-tickets"), "bulk-actions")
	bulk.AddCommand(newCrmNative134("bulk_options", "tag-options", ""), newCrmNative134("bulk_apply", "apply", ""), newCrmNative134("bulk_original", "original", "ticket"))
	group(crm, "documents").AddCommand(newCrmNative134("restore_original", "restore-original", "document"))
}

func newCrmNative134(operation, action, parameter string) *cobra.Command {
	var data, file, key, keyEnv, pipeline string
	var page, perPage int64
	var source, confirmed, dryRun, schema bool
	mutating := operation == "stage_update" || operation == "bulk_apply"
	original := strings.HasSuffix(operation, "_original")
	takesBody := mutating || original
	use := action
	if parameter != "" {
		use += " <" + parameter + ">"
	}
	c := &cobra.Command{Use: use, Short: "Contrato CRM nativo privado",
		Long: "Contrato nativo de Stage, Bulk y Restore Original. Una dispatch por comando, sin retry o redirect. Conserva el primer cuerpo y la misma clave literal; Original deriva sólo los selectores del dueño y usa GET sin cuerpo. Bulk conserva cada ticket/CAS, confirmed/rejected/unknown y can_retry=false. UNKNOWN se consulta con Original del mismo intento; nunca se reenvía la escritura ni se confirma desde Current. Las credenciales reales y la autoridad vigente del servidor siguen siendo necesarias; publicación y delivery OFF."}
	c.Args = UsageArgs(func(cmd *cobra.Command, args []string) error {
		count := 0
		if parameter != "" {
			count = 1
		}
		if schema {
			return cobra.MaximumNArgs(count)(cmd, args)
		}
		return cobra.ExactArgs(count)(cmd, args)
	})
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if !source || !cmd.Flags().Changed("source-contracts") {
			return apierr.Usagef("requiere --source-contracts explícito; contrato nativo privado y delivery OFF")
		}
		contract, err := spec.NativeCrm134Contract()
		if err != nil {
			return err
		}
		op := contract.Operations[operation]
		if schema {
			result := map[string]any{"native_sha256": contract.NativeHash, "operation": op, "components": contract.Components, "published": false, "ratified": false, "delivery_enabled": false, "capabilities": []string{}}
			if original {
				inputOperation := map[string]string{"stage_original": "stage_update", "bulk_original": "bulk_apply", "restore_original": "restore_intent"}[operation]
				result["original_intent_boundary"] = contract.Operations[inputOperation]
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		identity := ""
		if parameter != "" {
			identity = args[0]
		}
		if key != "" && keyEnv != "" {
			return apierr.Usagef("elige una sola fuente de clave literal")
		}
		if keyEnv != "" {
			key = os.Getenv(keyEnv)
		}
		var raw []byte
		if takesBody {
			if (data == "") == (file == "") {
				return apierr.Usagef("elige exactamente --data o --data-file; Original requiere el cuerpo del primer intento")
			}
			if file != "" {
				raw, err = readInputFile(file)
			} else {
				raw, err = readDataArg(data, cmd.InOrStdin())
			}
			if err != nil {
				return apierr.Usagef("no se pudo leer el cuerpo local; no se envió")
			}
		}
		query := url.Values{}
		if operation == "stage_list" {
			if cmd.Flags().Changed("pipeline-id") {
				query.Set("pipeline_id", pipeline)
			}
			if cmd.Flags().Changed("page") {
				query.Set("page", strconv.FormatInt(page, 10))
			}
			if cmd.Flags().Changed("per-page") {
				query.Set("per_page", strconv.FormatInt(perPage, 10))
			}
		}
		if err := client.ValidateCrmNative134Intent(operation, identity, raw, key, query); err != nil {
			return err
		}
		if dryRun {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"mode": "dry-run", "method": op.Method, "route_template": "/v1" + op.Path, "required_scope": op.Scope(), "native_sha256": contract.NativeHash, "network_calls": 0, "published": false})
		}
		if mutating && (!confirmed || !cmd.Flags().Changed("confirmed")) {
			return apierr.Usagef("requiere --confirmed humano explícito para este intento/CAS")
		}
		cc, cleanup, err := crmNativeSourceContext(cmd, source, false, operation, contract)
		if err != nil {
			return err
		}
		defer cleanup()
		if mutating {
			if err := safety.RequireLive(cc.res.Environment, globalsFrom(cmd).Live); err != nil {
				return err
			}
		}
		response, err := cc.client.CrmNative134Request(cmd.Context(), operation, identity, raw, key, query)
		if err != nil {
			return serviceLevelReport(cmd, cc, err)
		}
		return output.PrintBody(cmd.OutOrStdout(), response.Body, cc.format)
	}
	c.Flags().BoolVar(&source, "source-contracts", false, "opta por contrato nativo privado, sin grants/release")
	c.Flags().BoolVar(&schema, "schema", false, "imprime operación/refs nativos, sin auth/HTTP")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "valida el mismo intento/selectores sin auth/HTTP o datos sensibles en salida")
	if takesBody {
		c.Flags().StringVar(&data, "data", "", "bytes JSON (@file o - stdin) del primer intento, sin reescribir")
		c.Flags().StringVar(&file, "data-file", "", "fichero original sólo leído")
		c.Flags().StringVar(&key, "idempotency-key", "", "clave ASCII explícita literal representable; no se genera otra")
		c.Flags().StringVar(&keyEnv, "idempotency-key-env", "", "nombre del entorno con la misma clave literal fuera de argv")
	}
	if mutating {
		c.Flags().BoolVar(&confirmed, "confirmed", false, "confirma humanamente la selección/identidad/CAS/intención")
	}
	if operation == "stage_list" {
		c.Flags().StringVar(&pipeline, "pipeline-id", "", "UUID opaco del pipeline autorizado")
		c.Flags().Int64Var(&page, "page", 1, "página del contrato nativo")
		c.Flags().Int64Var(&perPage, "per-page", 100, "tamaño de página del contrato nativo")
	}
	return c
}

func crmNative134Manifest() []manifestEntry {
	contract, err := spec.NativeCrm134Contract()
	if err != nil {
		return nil
	}
	unpublished := false
	commands := []struct{ key, path, arg string }{
		{"stage_list", "crm service-stages list", ""},
		{"stage_update", "crm service-stages update", "stage"},
		{"stage_original", "crm service-stages original", "stage"},
		{"bulk_options", "crm service-tickets bulk-actions tag-options", ""},
		{"bulk_apply", "crm service-tickets bulk-actions apply", ""},
		{"bulk_original", "crm service-tickets bulk-actions original", "ticket"},
		{"restore_original", "crm documents restore-original", "document"},
	}
	entries := []manifestEntry{}
	for _, command := range commands {
		op := contract.Operations[command.key]
		entry := manifestEntry{Command: "factuarea " + command.path, OperationID: op.ID(), Summary: "Contrato nativo privado; Original conserva el intento sin reenvío", RequiredScope: op.Scope(), Mutating: op.Method != http.MethodGet, Flags: []flagInfo{{Name: "source-contracts", Type: "boolean"}, {Name: "schema", Type: "boolean"}, {Name: "dry-run", Type: "boolean"}}, ContractState: "unreleased", Published: &unpublished, ContractSHA256: contract.NativeHash}
		if command.arg != "" {
			entry.Args = []string{command.arg}
		}
		if entry.Mutating || strings.HasSuffix(command.key, "_original") {
			entry.Flags = append(entry.Flags, flagInfo{Name: "data", Type: "string"}, flagInfo{Name: "data-file", Type: "string"}, flagInfo{Name: "idempotency-key", Type: "string"}, flagInfo{Name: "idempotency-key-env", Type: "string"})
		}
		if entry.Mutating {
			entry.Flags = append(entry.Flags, flagInfo{Name: "confirmed", Type: "boolean"})
		}
		if command.key == "stage_list" {
			entry.Flags = append(entry.Flags, flagInfo{Name: "pipeline-id", Type: "string"}, flagInfo{Name: "page", Type: "integer"}, flagInfo{Name: "per-page", Type: "integer"})
		}
		entries = append(entries, entry)
	}
	return entries
}
