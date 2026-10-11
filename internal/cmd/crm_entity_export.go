package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/factuarea/factuarea-cli/internal/spec"
	"github.com/spf13/cobra"
)

func registerCrmEntityExportCommands(root *cobra.Command) {
	var crm *cobra.Command
	for _, child := range root.Commands() {
		if child.Name() == "crm" {
			crm = child
			break
		}
	}
	if crm == nil {
		crm = serviceLevelGroup("crm", "Comandos CRM")
		root.AddCommand(crm)
	}
	group := serviceLevelGroup("entity-exports", "Exportación general del universo nativo autorizado")
	group.Long = "organization, person, lead, opportunity, activity y service_ticket. Filtros, período y columnas proceden del dueño. El servidor comprueba scopes, C7 y máscaras actuales; esta lista estática no concede permiso."
	group.AddCommand(newCrmEntityExportRequest(), newCrmEntityExportOriginal(), newCrmEntityExportDownload())
	crm.AddCommand(group)
}

func crmEntityExportContext(cmd *cobra.Command, skipScope bool) (*cliContext, func(), error) {
	g := globalsFrom(cmd)
	cc, err := newCLIContext(g, "")
	if err != nil {
		return nil, nil, err
	}
	opts, err := baseURLClientOptions(g.AllowInsecureTransport)
	if err != nil {
		return nil, nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	hc := &http.Client{Timeout: 60 * time.Second, Transport: serviceLevelSingleDispatch{transport},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	opts = append(opts, client.WithMaxRetries(0), client.WithHTTPClient(hc))
	cc.client = client.New(cc.res.APIKey, opts...)
	cleanup := transport.CloseIdleConnections
	if !skipScope {
		scopes, err := cc.scopes(cmd.Context())
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		if !safety.HasScope(scopes, spec.NativeCrmEntityExportContract().Operations["original"].RequiredScope) {
			cleanup()
			return nil, nil, apierr.Permf("la credencial actual no publica crm_imports:export; el servidor conserva C7 y máscaras")
		}
	}
	return cc, cleanup, nil
}

func newCrmEntityExportRequest() *cobra.Command {
	var data, dataFile, key string
	var confirmed, skipScope bool
	c := &cobra.Command{Use: "request", Short: "Solicita una exportación general confirmada explícitamente", Args: UsageArgs(cobra.NoArgs),
		Long: "Declara un JSON cerrado con id UUIDv7 estable, entity_type, filters objeto, columns lista ordenada y confirmed:true. --confirmed acredita tu confirmación explícita; --idempotency-key conserva la clave original. Sólo se envía un POST, sin replay ni redirects. Ante timeout consulta original/status con el mismo id. El CLI no guarda selección, credenciales ni un checkpoint de PII."}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		if !confirmed || !cmd.Flags().Changed("confirmed") {
			return apierr.Usagef("request exige --confirmed explícito tras confirmar filtros y columnas")
		}
		if (data == "" && dataFile == "") || (data != "" && dataFile != "") {
			return apierr.Usagef("declara una sola fuente original: --data o --data-file")
		}
		var body []byte
		var err error
		if dataFile != "" {
			body, err = readInputFile(dataFile)
		} else {
			body, err = readDataArg(data, cmd.InOrStdin())
		}
		if err != nil {
			return err
		}
		selection, err := validateCrmEntityExportRequest(body)
		if err != nil {
			return err
		}
		if err := validateServiceLevelIdempotencyKey(key); err != nil {
			return err
		}
		cc, cleanup, err := crmEntityExportContext(cmd, skipScope)
		if err != nil {
			return err
		}
		defer cleanup()
		if err := safety.RequireLive(cc.res.Environment, globalsFrom(cmd).Live); err != nil {
			return err
		}
		op := spec.NativeCrmEntityExportContract().Operations["request"]
		response, err := cc.client.Do(cmd.Context(), op.Method, op.Path, body, map[string]string{"Idempotency-Key": key})
		if err != nil {
			var transportError *apierr.TransportError
			if errors.As(err, &transportError) || (response != nil && (response.StatusCode >= 500 || response.StatusCode >= 300 && response.StatusCode < 400)) {
				return serviceLevelReport(cmd, cc, crmEntityExportUnconfirmed(selection.id))
			}
			return serviceLevelReport(cmd, cc, err)
		}
		job, err := decodeCrmEntityExportJob(response.Body, selection.id, selection.entity)
		if err != nil || (job.Status == "processing" && response.StatusCode != http.StatusAccepted) || (job.Status != "processing" && response.StatusCode != http.StatusOK) {
			return serviceLevelReport(cmd, cc, crmEntityExportUnconfirmed(selection.id))
		}
		return output.PrintBody(cmd.OutOrStdout(), response.Body, cc.format)
	}
	c.Flags().StringVarP(&data, "data", "d", "", "JSON original (@fichero o - para stdin); sus bytes no se normalizan")
	c.Flags().StringVar(&dataFile, "data-file", "", "fichero del JSON original; el CLI sólo lo lee")
	c.Flags().StringVar(&key, "idempotency-key", "", "clave ASCII original obligatoria; no se genera ni sustituye")
	c.Flags().BoolVar(&confirmed, "confirmed", false, "confirma explícitamente la selección humana actual")
	c.Flags().BoolVar(&skipScope, "skip-scope-check", false, "omite sólo el preflight local; el servidor conserva su admisión actual")
	return c
}

func crmEntityExportUnconfirmed(id string) error {
	return &apierr.TransportError{Err: fmt.Errorf("resultado sin confirmar para export %s; conserva id/cuerpo/key y consulta `factuarea crm entity-exports original %s`; no se reenvió el POST", id, id)}
}

func crmEntityExportID(value string) (string, error) {
	if !serviceLevelUUID.MatchString(value) {
		return "", apierr.Usagef("export debe ser el UUIDv7 original")
	}
	return strings.ToLower(value), nil
}

func getCrmEntityExportOriginal(cmd *cobra.Command, cc *cliContext, id string) (crmEntityExportJob, []byte, error) {
	var job crmEntityExportJob
	op := spec.NativeCrmEntityExportContract().Operations["original"]
	path := strings.ReplaceAll(op.Path, "{export}", id)
	response, err := cc.client.Do(cmd.Context(), op.Method, path, nil, nil)
	if err != nil {
		return job, nil, err
	}
	if response.StatusCode != http.StatusOK {
		return job, nil, &apierr.TransportError{Err: fmt.Errorf("Original requiere la respuesta nativa HTTP200")}
	}
	job, err = decodeCrmEntityExportJob(response.Body, id, "")
	return job, response.Body, err
}

func newCrmEntityExportOriginal() *cobra.Command {
	var skipScope bool
	c := &cobra.Command{Use: "original <export>", Aliases: []string{"status"}, Short: "Consulta el mismo trabajo Original y su estado factual", Args: UsageArgs(cobra.ExactArgs(1)),
		Long: "GET read-only del UUID original. status es un alias de este mismo control. Processing, ready, failed y expired proceden del dueño; la consulta no reenvía, reencola ni convierte un estado en permiso."}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id, err := crmEntityExportID(args[0])
		if err != nil {
			return err
		}
		cc, cleanup, err := crmEntityExportContext(cmd, skipScope)
		if err != nil {
			return err
		}
		defer cleanup()
		_, body, err := getCrmEntityExportOriginal(cmd, cc, id)
		if err != nil {
			return serviceLevelReport(cmd, cc, err)
		}
		return output.PrintBody(cmd.OutOrStdout(), body, cc.format)
	}
	c.Flags().BoolVar(&skipScope, "skip-scope-check", false, "omite sólo el preflight local; el servidor conserva su admisión actual")
	return c
}

func newCrmEntityExportDownload() *cobra.Command {
	var outputPath string
	var skipScope bool
	c := &cobra.Command{Use: "download <export>", Short: "Descarga en streaming el CSV nativo listo y reautorizado", Args: UsageArgs(cobra.ExactArgs(1)),
		Long: "Consulta Original fresco, exige ready y después consume el GET binario nativo con nombre, MIME y tamaño originales. No genera CSV ni recorre páginas. Sin -o, stdout requiere pipe; un error de transporte puede dejar salida parcial. -o crea un fichero privado nuevo y elimina el parcial al fallar; no sobreescribe otros datos. No se guardan selección ni credenciales."}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id, err := crmEntityExportID(args[0])
		if err != nil {
			return err
		}
		if outputPath == "" && output.IsTTY(os.Stdout) {
			return apierr.Usagef("la descarga es binaria; usa -o <fichero nuevo> o un pipe")
		}
		cc, cleanup, err := crmEntityExportContext(cmd, skipScope)
		if err != nil {
			return err
		}
		defer cleanup()
		job, _, err := getCrmEntityExportOriginal(cmd, cc, id)
		if err != nil {
			return serviceLevelReport(cmd, cc, err)
		}
		if job.Status != "ready" {
			return apierr.Usagef("el Original tiene estado %s; sólo ready acredita el fichero nativo", job.Status)
		}
		op := spec.NativeCrmEntityExportContract().Operations["download"]
		path := strings.ReplaceAll(op.Path, "{export}", id)
		stream := func(w io.Writer) error {
			return cc.client.StreamCrmEntityExport(cmd.Context(), path, job.Filename, job.Size, w)
		}
		if outputPath == "" {
			err = stream(cmd.OutOrStdout())
		} else {
			err = streamCrmEntityExportFile(outputPath, stream)
		}
		if err != nil {
			return serviceLevelReport(cmd, cc, err)
		}
		return nil
	}
	c.Flags().StringVarP(&outputPath, "output", "o", "", "fichero nuevo elegido explícitamente; permisos 0600, sin overwrite ni caché adicional")
	c.Flags().BoolVar(&skipScope, "skip-scope-check", false, "omite sólo el preflight local; el servidor conserva su admisión actual")
	return c
}

func streamCrmEntityExportFile(path string, stream func(io.Writer) error) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return apierr.WriteFileUsagef(path, err)
	}
	complete := false
	defer func() {
		_ = f.Close()
		if !complete {
			_ = os.Remove(path)
		}
	}()
	if err := stream(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return apierr.WriteFileUsagef(path, err)
	}
	if err := f.Close(); err != nil {
		return apierr.WriteFileUsagef(path, err)
	}
	complete = true
	return nil
}

func crmEntityExportManifest() []manifestEntry {
	contract := spec.NativeCrmEntityExportContract()
	entries := []manifestEntry{}
	for _, action := range []string{"request", "original", "download", "status"} {
		operation := action
		if action == "status" {
			operation = "original"
		}
		op := contract.Operations[operation]
		entry := manifestEntry{Command: "factuarea crm entity-exports " + action, OperationID: op.OperationID,
			Summary: "Exportación general nativa: " + action, Args: []string{}, Flags: []flagInfo{{Name: "skip-scope-check", Type: "bool"}},
			RequiredScope: op.RequiredScope, Mutating: action == "request", Binary: action == "download"}
		if action == "request" {
			entry.Flags = append(entry.Flags, flagInfo{Name: "data", Type: "string"}, flagInfo{Name: "data-file", Type: "string"}, flagInfo{Name: "idempotency-key", Type: "string"}, flagInfo{Name: "confirmed", Type: "bool"})
			entry.BodyFields = []manifestField{{Name: "id", Type: "string", Kind: "scalar", Required: true},
				{Name: "entity_type", Type: "string", Kind: "scalar", Required: true, Enum: contract.Types},
				{Name: "filters", Type: "object", Kind: "map", Required: true}, {Name: "columns", Type: "string", Kind: "scalar_array", Required: true},
				{Name: "confirmed", Type: "boolean", Kind: "scalar", Required: true}}
		} else {
			entry.Args = []string{"export"}
		}
		if action == "download" {
			entry.Flags = append(entry.Flags, flagInfo{Name: "output", Type: "string"})
		}
		entries = append(entries, entry)
	}
	return entries
}
