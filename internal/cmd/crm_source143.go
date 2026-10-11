package cmd

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/factuarea/factuarea-cli/internal/spec"
	"github.com/spf13/cobra"
)

func registerCrmSource143Commands(root *cobra.Command) {
	group := func(parent *cobra.Command, name string) *cobra.Command {
		for _, child := range parent.Commands() {
			if child.Name() == name {
				return child
			}
		}
		child := serviceLevelGroup(name, "Controles nativos Source privados")
		parent.AddCommand(child)
		return child
	}
	crm := group(root, "crm")
	imports := group(crm, "imports")
	imports.AddCommand(newCrmSource143("import_apply", "source-apply"), newCrmSource143("import_current", "source-current"), newCrmSource143("import_original", "source-original"))
	documents := group(crm, "documents")
	documents.AddCommand(newCrmSource143("document_restore", "restore"), newCrmSource143("document_current", "current"))
}

func newCrmSource143(operation, action string) *cobra.Command {
	var data, file, key, keyEnv string
	var source, confirmed, dryRun, schema bool
	parameter := "run"
	if strings.HasPrefix(operation, "document_") {
		parameter = "document"
	}
	mutating := operation == "import_apply" || operation == "document_restore"
	takesBody := mutating || operation == "import_original"
	c := &cobra.Command{Use: action + " <" + parameter + ">", Args: UsageArgs(cobra.ExactArgs(1)), Short: "Control nativo privado Source143",
		Long: "Contrato nativo Source143 sin publicación/activación CRM. Cuerpo y clave explícitos conservan la intención. Current se relee antes de escribir y su CAS debe coincidir; Apply también conserva preview_hash. Una dispatch, sin retry o redirect. Apply acepta sólo202 y dispone de Original GET separado. Restore acepta sólo200: no existe Original GET de Restore en este contrato, por lo que UNKNOWN se detiene sin reenviar ni inferir desde Current."}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if !source || !cmd.Flags().Changed("source-contracts") {
			return apierr.Usagef("requiere --source-contracts explícito; contrato nativo privado y delivery OFF")
		}
		contract, err := spec.NativeCrmSource143Contract()
		if err != nil {
			return err
		}
		op := contract.Operations[operation]
		if schema {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"native_sha256": contract.NativeHash, "operation": op, "components": contract.Components, "published": false})
		}
		if contract.ValidateParameter(operation, "path", parameter, args[0]) != nil {
			return apierr.Usagef("identidad fuera del UUID nativo")
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
				return apierr.Usagef("elige exactamente --data o --data-file; Original conserva el primer raw")
			}
			if file != "" {
				raw, err = readInputFile(file)
			} else {
				raw, err = readDataArg(data, cmd.InOrStdin())
			}
			if err != nil {
				return apierr.Usagef("no se pudo leer el cuerpo local; no se envió")
			}
			if contract.ValidateParameter(operation, "header", "Idempotency-Key", key) != nil {
				return apierr.Usagef("se requiere clave literal original")
			}
			if mutating && contract.ValidateRequest(operation, raw) != nil {
				return apierr.Usagef("cuerpo incompatible con el esquema cerrado nativo")
			}
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
			currentKey := "import_current"
			if operation == "document_restore" {
				currentKey = "document_current"
			}
			current, err := cc.client.CrmSource143Request(cmd.Context(), currentKey, args[0], nil, "")
			if err != nil {
				return serviceLevelReport(cmd, cc, err)
			}
			if err := crmSource143CurrentCAS(operation, args[0], raw, current.Body); err != nil {
				return err
			}
		}
		response, err := cc.client.CrmSource143Request(cmd.Context(), operation, args[0], raw, key)
		if err != nil {
			return serviceLevelReport(cmd, cc, err)
		}
		return output.PrintBody(cmd.OutOrStdout(), response.Body, cc.format)
	}
	c.Flags().BoolVar(&source, "source-contracts", false, "opta por Source nativo privado, sin grants/release")
	c.Flags().BoolVar(&schema, "schema", false, "imprime operación/refs reales, sin auth/HTTP")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "valida plantilla sin HTTP/cuerpo/clave/identidad en salida")
	if takesBody {
		c.Flags().StringVar(&data, "data", "", "bytes JSON (@file o - stdin), sin normalizar")
		c.Flags().StringVar(&file, "data-file", "", "fichero original sólo leído")
		c.Flags().StringVar(&key, "idempotency-key", "", "clave explícita literal; no se genera otra")
		c.Flags().StringVar(&keyEnv, "idempotency-key-env", "", "nombre del entorno con clave literal fuera de argv")
	}
	if mutating {
		c.Flags().BoolVar(&confirmed, "confirmed", false, "confirma humanamente cuerpo/identidad/CAS/intención")
	}
	return c
}

func crmSource143CurrentCAS(operation, identity string, raw, current []byte) error {
	var input struct {
		Expected json.Number `json:"expected_version"`
		Hash     string      `json:"preview_hash"`
	}
	var head struct {
		Data struct {
			ID      string      `json:"id"`
			Version json.Number `json:"version"`
			Hash    string      `json:"preview_hash"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &input) != nil || json.Unmarshal(current, &head) != nil || !strings.EqualFold(head.Data.ID, identity) {
		return apierr.Permf("Current no permite comprobar este target/CAS; no se envió")
	}
	expected, err := strconv.ParseInt(string(input.Expected), 10, 64)
	version, headErr := strconv.ParseInt(string(head.Data.Version), 10, 64)
	if err != nil || headErr != nil || version < 1 || expected != version {
		return apierr.Usagef("expected_version no coincide con Current; relee y confirma una intención nueva, no se envió")
	}
	if operation == "import_apply" && (input.Hash == "" || input.Hash != head.Data.Hash) {
		return apierr.Usagef("preview_hash no coincide con Current; no se envió Apply")
	}
	return nil
}

func crmSource143Manifest() []manifestEntry {
	contract, err := spec.NativeCrmSource143Contract()
	if err != nil {
		return nil
	}
	unpublished := false
	commands := []struct{ key, path, arg string }{
		{"import_apply", "crm imports source-apply", "run"}, {"import_current", "crm imports source-current", "run"}, {"import_original", "crm imports source-original", "run"}, {"document_restore", "crm documents restore", "document"}, {"document_current", "crm documents current", "document"},
	}
	entries := []manifestEntry{}
	for _, command := range commands {
		op := contract.Operations[command.key]
		entry := manifestEntry{Command: "factuarea " + command.path, OperationID: op.ID(), Summary: "Source143 nativo privado; Current no confirma Original", Args: []string{command.arg}, RequiredScope: op.Scope(), Mutating: op.Method == "POST", Flags: []flagInfo{{Name: "source-contracts", Type: "boolean"}, {Name: "schema", Type: "boolean"}, {Name: "dry-run", Type: "boolean"}}, ContractState: "unreleased", Published: &unpublished, ContractSHA256: contract.NativeHash}
		if entry.Mutating || command.key == "import_original" {
			entry.Flags = append(entry.Flags, flagInfo{Name: "data", Type: "string"}, flagInfo{Name: "data-file", Type: "string"}, flagInfo{Name: "idempotency-key", Type: "string"}, flagInfo{Name: "idempotency-key-env", Type: "string"})
		}
		if entry.Mutating {
			entry.Flags = append(entry.Flags, flagInfo{Name: "confirmed", Type: "boolean"})
		}
		entries = append(entries, entry)
	}
	return entries
}
