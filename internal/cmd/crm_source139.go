package cmd

import (
	"net/http"
	"net/url"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/factuarea/factuarea-cli/internal/spec"
	"github.com/spf13/cobra"
)

func registerCrmSource139Commands(root *cobra.Command) {
	group := func(parent *cobra.Command, name, summary string) *cobra.Command {
		for _, child := range parent.Commands() {
			if child.Name() == name {
				return child
			}
		}
		child := serviceLevelGroup(name, summary)
		parent.AddCommand(child)
		return child
	}
	crm := group(root, "crm", "Comandos CRM")
	imports := group(crm, "imports", "Lectores de importación CRM sobre contrato Source privado")
	imports.AddCommand(newCrmImportApplyOriginal139())
	group(root, "agenda", "Agenda nativa").AddCommand(newCrmAgendaRead139())
}

func crmSource139Context(cmd *cobra.Command, sourceEnabled, skipScope bool, key string) (*cliContext, func(), error) {
	if !sourceEnabled || !cmd.Flags().Changed("source-contracts") {
		return nil, nil, apierr.Usagef("este lector requiere --source-contracts explícito: el contrato nativo sigue privado, sin publicación o activación CRM")
	}
	contract, err := spec.NativeCrmSource139Contract()
	if err != nil {
		return nil, nil, err
	}
	cc, err := newCLIContext(globalsFrom(cmd), "")
	if err != nil {
		return nil, nil, err
	}
	opts, err := baseURLClientOptions(globalsFrom(cmd).AllowInsecureTransport)
	if err != nil {
		return nil, nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	hc := &http.Client{Timeout: 60 * time.Second, Transport: serviceLevelSingleDispatch{transport}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	opts = append(opts, client.WithMaxRetries(0), client.WithHTTPClient(hc))
	cc.client = client.New(cc.res.APIKey, opts...)
	cleanup := transport.CloseIdleConnections
	if !skipScope {
		scopes, err := cc.scopes(cmd.Context())
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		if !safety.HasScope(scopes, contract.Operations[key].Scope()) {
			cleanup()
			return nil, nil, apierr.Permf("la credencial actual no publica el scope del lector nativo; el servidor conserva Company, C7 y máscaras")
		}
	}
	return cc, cleanup, nil
}

func newCrmImportApplyOriginal139() *cobra.Command {
	var data, file, key string
	var sourceEnabled, skipScope bool
	c := &cobra.Command{Use: "apply-original <run>", Short: "Consulta el Original de Apply sin reenviar el POST", Args: UsageArgs(cobra.ExactArgs(1)),
		Long: "Contrato Source139 privado. Conserva UUID del run padre, Idempotency-Key y bytes exactos del primer POST (incluidos espacios/salto final). El CLI calcula SHA256 literal y sólo ejecuta GET Original. missing, in_progress y unconfirmed permanecen factuales; Current no confirma el claim. La respuesta original conserva su string body exacto, sin deserializarlo ni cambiarlo por una cabeza actual. No guarda cuerpos, credenciales o checkpoints, ni genera claves. No exige confirmación de escritura para este lector."}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if (data == "" && file == "") || (data != "" && file != "") {
			return apierr.Usagef("declara sólo --original-body o --original-body-file con los bytes del primer POST")
		}
		if err := validateServiceLevelIdempotencyKey(key); err != nil {
			return err
		}
		var raw []byte
		var err error
		if file != "" {
			raw, err = readInputFile(file)
		} else {
			raw, err = readDataArg(data, cmd.InOrStdin())
		}
		if err != nil {
			return err
		}
		contract, err := spec.NativeCrmSource139Contract()
		if err != nil {
			return err
		}
		if contract.ValidateParameter("import_apply_original", "path", "run", args[0]) != nil || len(raw) == 0 {
			return apierr.Usagef("conserva el UUID original y su cuerpo POST no vacío")
		}
		cc, cleanup, err := crmSource139Context(cmd, sourceEnabled, skipScope, "import_apply_original")
		if err != nil {
			return err
		}
		defer cleanup()
		_, response, err := cc.client.ReadCrmImportApplyOriginal(cmd.Context(), args[0], raw, key)
		if err != nil {
			return serviceLevelReport(cmd, cc, err)
		}
		return output.PrintBody(cmd.OutOrStdout(), response.Body, cc.format)
	}
	c.Flags().StringVar(&data, "original-body", "", "bytes JSON originales (@fichero o - stdin), sin normalizar")
	c.Flags().StringVar(&file, "original-body-file", "", "fichero original sólo leído; su SHA incluye todos los bytes")
	c.Flags().StringVar(&key, "idempotency-key", "", "clave literal original requerida; no genera una nueva ni recorta")
	c.Flags().BoolVar(&sourceEnabled, "source-contracts", false, "opta explícitamente por el contrato Source privado; no concede release/grants")
	c.Flags().BoolVar(&skipScope, "skip-scope-check", false, "omite sólo preflight local; conserva la autorización nativa del servidor")
	return c
}

func newCrmAgendaRead139() *cobra.Command {
	var from, to, sources, assignee string
	var sourceEnabled, skipScope bool
	c := &cobra.Command{Use: "source-read", Short: "Lee una ventana completa de agenda según el contrato nativo139", Args: UsageArgs(cobra.NoArgs),
		Long: "Sólo GET, una página nativa completa; no crea tareas ni llama calendarios externos. from/to son fechas incluidas (máximo 93 días). Sources omitido solicita todas las capas y sources vacío ninguna; el servidor omite las que no tienen scope/módulo. La salida conserva las fuentes realmente servidas, sin fabricar disponibilidad ni UUID para festivos/impuestos. El comando agenda list publicado permanece independiente."}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		query := url.Values{"from": []string{from}, "to": []string{to}}
		if cmd.Flags().Changed("sources") {
			query.Set("sources", sources)
		}
		if cmd.Flags().Changed("assignee-id") {
			query.Set("assignee_id", assignee)
		}
		cc, cleanup, err := crmSource139Context(cmd, sourceEnabled, skipScope, "agenda")
		if err != nil {
			return err
		}
		defer cleanup()
		response, err := cc.client.ReadCrmSource139Agenda(cmd.Context(), query)
		if err != nil {
			return serviceLevelReport(cmd, cc, err)
		}
		return output.PrintBody(cmd.OutOrStdout(), response.Body, cc.format)
	}
	c.Flags().StringVar(&from, "from", "", "primer día YYYY-MM-DD incluido")
	c.Flags().StringVar(&to, "to", "", "último día YYYY-MM-DD incluido; ventana máxima93")
	c.Flags().StringVar(&sources, "sources", "", "capas nativas separadas por coma; omitido todas, explícitamente vacío ninguna")
	c.Flags().StringVar(&assignee, "assignee-id", "", "me o UUID actual de miembro; filtra sólo tasks")
	c.Flags().BoolVar(&sourceEnabled, "source-contracts", false, "opta por este lector del contrato Source privado sin activar CRM")
	c.Flags().BoolVar(&skipScope, "skip-scope-check", false, "omite preflight local, no scopes/módulos/C7 del servidor")
	return c
}

func crmSource139Manifest() []manifestEntry {
	contract, err := spec.NativeCrmSource139Contract()
	if err != nil {
		return nil
	}
	unpublished := false
	return []manifestEntry{
		{Command: "factuarea crm imports apply-original", OperationID: contract.Operations["import_apply_original"].ID(), Summary: "Original Apply read-only, jamás Current o replay POST", Args: []string{"run"}, Flags: []flagInfo{{Name: "original-body", Type: "string"}, {Name: "original-body-file", Type: "string"}, {Name: "idempotency-key", Type: "string"}, {Name: "source-contracts", Type: "boolean"}}, RequiredScope: contract.Operations["import_apply_original"].Scope(), ContractState: "unreleased", Published: &unpublished, ContractSHA256: contract.NativeHash},
		{Command: "factuarea agenda source-read", OperationID: contract.Operations["agenda"].ID(), Summary: "Agenda GET de una sola página y capas realmente servidas", Args: []string{}, Flags: []flagInfo{{Name: "from", Type: "string"}, {Name: "to", Type: "string"}, {Name: "sources", Type: "string"}, {Name: "assignee-id", Type: "string"}, {Name: "source-contracts", Type: "boolean"}}, RequiredScope: contract.Operations["agenda"].Scope(), ContractState: "unreleased", Published: &unpublished, ContractSHA256: contract.NativeHash},
	}
}
