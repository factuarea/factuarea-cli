package cmd

import (
	"encoding/json"
	"os"
	"strconv"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/factuarea/factuarea-cli/internal/spec"
	"github.com/spf13/cobra"
)

func registerCrmJourney140Commands(root *cobra.Command) {
	var crm *cobra.Command
	for _, child := range root.Commands() {
		if child.Name() == "crm" {
			crm = child
		}
	}
	if crm == nil {
		crm = serviceLevelGroup("crm", "Comandos CRM")
		root.AddCommand(crm)
	}
	for _, entity := range []string{"lead", "opportunity", "activity"} {
		group := serviceLevelGroup(entity, "Viaje CRM sobre el contrato Source nativo privado")
		for _, action := range []string{"create", "current", "original"} {
			group.AddCommand(newCrmJourney140(entity, action))
		}
		if entity == "activity" {
			group.AddCommand(newCrmJourney140(entity, "update"))
		}
		crm.AddCommand(group)
	}
}

func newCrmJourney140(entity, action string) *cobra.Command {
	var data, file, key, keyEnv string
	var source, confirmed, dryRun, schema bool
	operation := entity + "_" + action
	readsIdentity := action == "current" || action == "update"
	use := action
	args := cobra.NoArgs
	if readsIdentity {
		use += " <" + entity + ">"
		args = cobra.ExactArgs(1)
	}
	c := &cobra.Command{Use: use, Args: UsageArgs(args), Short: "Paso " + entity + " " + action + " del contrato nativo privado",
		Long: "Source opt-in explícito, sin publicación o grants. Create/update requieren confirmación humana, clave literal y cuerpo JSON cerrado nativo. Una única dispatch, sin reintentos ni redirect. UNKNOWN no permite reenviar: conserva raw/key y consulta original (sólo create tiene Original público). Current relee autoridad/cabeza actual y nunca prueba Original. No se guardan cuerpos, claves ni receipts."}
	c.RunE = func(cmd *cobra.Command, argv []string) error {
		if !source || !cmd.Flags().Changed("source-contracts") {
			return apierr.Usagef("requiere --source-contracts explícito; el contrato permanece privado y CRM OFF")
		}
		contract, err := spec.NativeCrmSource139Contract()
		if err != nil {
			return err
		}
		op := contract.Operations[operation]
		if schema {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"native_sha256": contract.NativeHash, "operation": op, "components": contract.Components, "published": false})
		}
		if key != "" && keyEnv != "" {
			return apierr.Usagef("elige una única fuente de clave literal")
		}
		if keyEnv != "" {
			key = os.Getenv(keyEnv)
		}
		var raw []byte
		if action != "current" {
			if (data == "") == (file == "") {
				return apierr.Usagef("declara exactamente --data o --data-file; Original usa los mismos bytes del primer POST")
			}
			if file != "" {
				raw, err = readInputFile(file)
			} else {
				raw, err = readDataArg(data, cmd.InOrStdin())
			}
			if err != nil {
				return apierr.Usagef("no se pudo leer el cuerpo local; no se envió la intención")
			}
			if contract.ValidateParameter(operation, "header", "Idempotency-Key", key) != nil {
				return apierr.Usagef("la intención exige su clave nativa explícita")
			}
			if action != "original" && contract.ValidateRequest(operation, raw) != nil {
				return apierr.Usagef("el cuerpo no conserva el esquema cerrado nativo")
			}
		}
		identity := ""
		if readsIdentity {
			identity = argv[0]
			if contract.ValidateParameter(operation, "path", entity, identity) != nil {
				return apierr.Usagef("se requiere el UUID nativo del paso")
			}
		}
		if dryRun {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"mode": "dry-run", "method": op.Method, "route_template": "/v1" + op.Path, "required_scope": op.Scope(), "native_sha256": contract.NativeHash, "network_calls": 0, "published": false})
		}
		mutating := action == "create" || action == "update"
		if mutating && (!confirmed || !cmd.Flags().Changed("confirmed")) {
			return apierr.Usagef("requiere --confirmed explícito tras revisar cuerpo, referencias actuales y clave del intento")
		}
		cc, cleanup, err := crmSource139Context(cmd, source, false, operation)
		if err != nil {
			return err
		}
		defer cleanup()
		if mutating {
			if err := safety.RequireLive(cc.res.Environment, globalsFrom(cmd).Live); err != nil {
				return err
			}
		}
		if action == "update" {
			current, err := cc.client.CrmJourney140Request(cmd.Context(), entity+"_current", identity, nil, "")
			if err != nil {
				return serviceLevelReport(cmd, cc, err)
			}
			if err := crmJourney140CurrentCAS(raw, current.Body, identity); err != nil {
				return err
			}
		}
		response, err := cc.client.CrmJourney140Request(cmd.Context(), operation, identity, raw, key)
		if err != nil {
			return serviceLevelReport(cmd, cc, err)
		}
		return output.PrintBody(cmd.OutOrStdout(), response.Body, cc.format)
	}
	c.Flags().BoolVar(&source, "source-contracts", false, "opta por contrato Source privado sin activar CRM")
	c.Flags().BoolVar(&schema, "schema", false, "imprime operación/ref closure nativa, sin auth ni red")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "valida y muestra plantilla sin cuerpos/keys/identidades ni HTTP")
	if action != "current" {
		c.Flags().StringVar(&data, "data", "", "bytes JSON originales (@file o - stdin), sin normalizar")
		c.Flags().StringVar(&file, "data-file", "", "fichero de intención original sólo leído")
		c.Flags().StringVar(&key, "idempotency-key", "", "clave explícita literal; nunca se genera o cambia")
		c.Flags().StringVar(&keyEnv, "idempotency-key-env", "", "nombre del entorno que contiene la clave literal, fuera de argv")
	}
	if action == "create" || action == "update" {
		c.Flags().BoolVar(&confirmed, "confirmed", false, "confirmación humana explícita de este intento")
	}
	return c
}

func crmJourney140CurrentCAS(raw, current []byte, identity string) error {
	var input struct {
		Expected json.Number `json:"expected_version"`
	}
	var head struct {
		Data struct {
			Activity *struct {
				ID      string      `json:"id"`
				Version json.Number `json:"version"`
			} `json:"activity"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &input) != nil || json.Unmarshal(current, &head) != nil || head.Data.Activity == nil || head.Data.Activity.ID != identity {
		return apierr.Permf("la cabeza actual no permite comprobar CAS; no se envió el PATCH")
	}
	expected, err := strconv.ParseInt(string(input.Expected), 10, 64)
	version, headErr := strconv.ParseInt(string(head.Data.Activity.Version), 10, 64)
	if err != nil || headErr != nil || version < 1 || expected != version {
		return apierr.Usagef("expected_version no coincide con Current; relee y confirma un nuevo intento, no se envió el PATCH")
	}
	return nil
}

func crmJourney140Manifest() []manifestEntry {
	contract, err := spec.NativeCrmSource139Contract()
	if err != nil {
		return nil
	}
	unpublished := false
	entries := []manifestEntry{}
	for _, entity := range []string{"lead", "opportunity", "activity"} {
		actions := []string{"create", "current", "original"}
		if entity == "activity" {
			actions = append(actions, "update")
		}
		for _, action := range actions {
			op := contract.Operations[entity+"_"+action]
			entry := manifestEntry{Command: "factuarea crm " + entity + " " + action, OperationID: op.ID(), Summary: "Viaje Source privado; Current y Original separados", Args: []string{}, Flags: []flagInfo{{Name: "source-contracts", Type: "boolean"}, {Name: "schema", Type: "boolean"}, {Name: "dry-run", Type: "boolean"}}, RequiredScope: op.Scope(), Mutating: action == "create" || action == "update", ContractState: "unreleased", Published: &unpublished, ContractSHA256: contract.NativeHash}
			if action == "current" || action == "update" {
				entry.Args = []string{entity}
			}
			if action != "current" {
				entry.Flags = append(entry.Flags, flagInfo{Name: "data", Type: "string"}, flagInfo{Name: "data-file", Type: "string"}, flagInfo{Name: "idempotency-key", Type: "string"}, flagInfo{Name: "idempotency-key-env", Type: "string"})
			}
			if entry.Mutating {
				entry.Flags = append(entry.Flags, flagInfo{Name: "confirmed", Type: "boolean"})
			}
			entries = append(entries, entry)
		}
	}
	return entries
}
