package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type crmServiceTicketOperation struct {
	action, operationID, method, path, summary string
	args                                       []string
	paginated                                  bool
}

var crmServiceTicketOperations = []crmServiceTicketOperation{
	{"stage", "moveServiceTicketToStage", "POST", "/v1/crm/service-tickets/{ticket}/move-to-stage", "Mueve un ticket a una etapa nativa con CAS", []string{"ticket"}, false},
	{"attach", "attachServiceTicketDocument", "POST", "/v1/crm/service-tickets/{ticket}/attachments", "Asocia una versión nativa de documento con CAS", []string{"ticket"}, false},
	{"detach", "detachServiceTicketDocument", "POST", "/v1/crm/service-tickets/{ticket}/attachments/remove", "Retira una asociación existente con CAS", []string{"ticket"}, false},
	{"get", "getServiceTicket", "GET", "/v1/crm/service-tickets/{ticket}", "Consulta los campos autorizados de un ticket", []string{"ticket"}, false},
	{"list", "listServiceTickets", "GET", "/v1/crm/service-tickets", "Lista los tickets autorizados", nil, true},
	{"timeline", "getServiceTicketTimeline", "GET", "/v1/crm/service-tickets/{ticket}/timeline", "Consulta las revisiones históricas autorizadas de un ticket", []string{"ticket"}, true},
}

var crmServiceTicketExamples = map[string]string{
	"stage":  `{"expected_version":1,"pipeline_id":"01900000-0000-7000-8000-000000000001","stage_id":"01900000-0000-7000-8000-000000000002"}`,
	"attach": `{"expected_version":1,"document_id":"01900000-0000-7000-8000-000000000002","document_version":1}`,
	"detach": `{"expected_version":1,"attachment_id":"01900000-0000-7000-8000-000000000002"}`,
}

func (op crmServiceTicketOperation) mutating() bool { return op.method == http.MethodPost }
func (op crmServiceTicketOperation) scope() string {
	if op.mutating() {
		return "customer_service:write"
	}
	return "customer_service:read"
}
func (op crmServiceTicketOperation) validateArgs(args []string) error {
	if len(args) != len(op.args) {
		return apierr.Usagef("número de identificadores incompatible con %s", op.action)
	}
	for _, arg := range args {
		if !crmDocumentID(arg, false) {
			return apierr.Usagef("ticket debe ser un UUIDv7")
		}
	}
	return nil
}
func (op crmServiceTicketOperation) pathFor(args []string) string {
	path := op.path
	for i, arg := range args {
		path = strings.ReplaceAll(path, "{"+op.args[i]+"}", arg)
	}
	return path
}

func registerCRMServiceTicketCommands(root *cobra.Command) {
	var crm *cobra.Command
	for _, command := range root.Commands() {
		if command.Name() == "crm" {
			crm = command
			break
		}
	}
	if crm == nil {
		crm = serviceLevelGroup("crm", "Comandos CRM")
		root.AddCommand(crm)
	}
	group := serviceLevelGroup("service-tickets", "Tickets de servicio sobre las APIs v1 JSON nativas")
	for _, op := range crmServiceTicketOperations {
		group.AddCommand(newCRMServiceTicketCommand(op))
	}
	group.AddCommand(newCRMServiceTicketRecoverCommand())
	crm.AddCommand(group)
}

type crmServiceTicketFlags struct {
	data, dataFile, key, intentFile, apiVersion, activeProfile, status, sort, direction string
	dryRun, skeleton, skipScopeCheck, all                                               bool
	page, perPage, maxPages                                                             int
}

func newCRMServiceTicketCommand(op crmServiceTicketOperation) *cobra.Command {
	f := &crmServiceTicketFlags{}
	use := op.action
	for _, arg := range op.args {
		use += " <" + arg + ">"
	}
	long := op.summary + "\n\nCompany, actor y entorno proceden de la credencial nativa (--profile / FACTUAREA_API_KEY)."
	if op.mutating() {
		long += "\nCuerpo JSON cerrado y clave original obligatoria. Se guarda un intent sellado privado (0600) antes del único envío. recover exige el mismo servidor, credencial, entorno y perfil; conserva UUID, CAS, bytes y key sin consultar una revisión nueva.\nPlantilla:\n  " + crmServiceTicketExamples[op.action]
	}
	if op.action == "stage" {
		long += "\nwaiting_reason omitido conserva la causa; waiting_customer abre la espera; null la libera. customer_message_id no nulo exige waiting_reason:null y una referencia factual nativa. La etapa de destino determina el estado; no se envían actor, estado ni fechas inventadas."
	}
	if op.action == "detach" {
		long += "\nattachment_id identifica una asociación obtenida de una lectura autorizada. La retirada conserva el documento nativo."
	}
	if op.paginated {
		long += "\n--all emite NDJSON autorizado hasta --max-pages. Un fallo deja salida parcial; las páginas no constituyen un snapshot."
	}
	c := &cobra.Command{Use: use, Short: op.summary, Long: long, Args: UsageArgs(cobra.ExactArgs(len(op.args)))}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if err := op.validateArgs(args); err != nil {
			return err
		}
		if err := validateCRMDocumentHeaders(f.apiVersion, f.activeProfile); err != nil {
			return err
		}
		var body []byte
		var request map[string]any
		if op.mutating() {
			if (f.data != "" && f.dataFile != "") || (f.skeleton && (f.data != "" || f.dataFile != "" || f.dryRun)) {
				return apierr.Usagef("elige --data, --data-file o --skeleton")
			}
			if f.skeleton {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), crmServiceTicketExamples[op.action])
				return err
			}
			var err error
			if f.dataFile != "" {
				body, err = readInputFile(f.dataFile)
			} else if f.data != "" {
				body, err = readDataArg(f.data, cmd.InOrStdin())
			} else {
				return apierr.Usagef("declara --data o --data-file; --skeleton muestra la plantilla")
			}
			if err != nil {
				return err
			}
			request, err = validateCRMServiceTicketBody(op.action, body)
			if err != nil {
				return err
			}
			if f.dryRun {
				return output.PrintBody(cmd.OutOrStdout(), body, output.JSON)
			}
			if err := crmDocumentKey(f.key); err != nil {
				return err
			}
			if f.intentFile == "" || f.intentFile == "-" {
				return apierr.Usagef("--intent-file nuevo es obligatorio antes de enviar una mutación")
			}
		}
		query, err := crmServiceTicketQuery(cmd, op, f)
		if err != nil {
			return err
		}
		cc, closeClient, err := newCRMDocumentContext(cmd, f.apiVersion, f.activeProfile)
		if err != nil {
			return err
		}
		defer closeClient()
		if err := authorizeCRMServiceTicket(cmd, cc, op, f.skipScopeCheck); err != nil {
			return err
		}
		if op.mutating() {
			intent := newCRMServiceTicketIntent(op, args, body, f.key, f.apiVersion, f.activeProfile, cc)
			if err := intent.save(f.intentFile); err != nil {
				return err
			}
			return dispatchCRMServiceTicketWrite(cmd, cc, op, intent, request, f.intentFile)
		}
		if f.all {
			return runCRMServiceTicketPages(cmd, cc, op, args, query, f)
		}
		resp, err := cc.client.Do(cmd.Context(), op.method, crmDocumentQueryPath(op.pathFor(args), query), nil, crmDocumentHeaders(f.apiVersion, f.activeProfile))
		if err != nil {
			return reportCRMDocument(cmd, cc, err)
		}
		data, err := crmServiceTicketValidatedRead(op, args, query, resp)
		if err != nil {
			return reportCRMDocument(cmd, cc, err)
		}
		return printCRMServiceTicketBody(cmd.OutOrStdout(), resp.Body, data, cc.format, op)
	}
	c.Flags().BoolVar(&f.skipScopeCheck, "skip-scope-check", false, "omite preflight local; la API revalida autoridad efectiva")
	c.Flags().StringVar(&f.apiVersion, "api-version", "", "Factuarea-Version original (YYYY-MM-DD)")
	c.Flags().StringVar(&f.activeProfile, "active-profile", "", "X-Active-Profile nativo autorizado por el servidor")
	if op.mutating() {
		c.Flags().StringVarP(&f.data, "data", "d", "", "cuerpo JSON original (@fichero o - para stdin)")
		c.Flags().StringVar(&f.dataFile, "data-file", "", "fichero con el cuerpo JSON original")
		c.Flags().StringVar(&f.key, "idempotency-key", "", "clave ASCII original obligatoria (1–255 caracteres)")
		c.Flags().StringVar(&f.intentFile, "intent-file", "", "checkpoint nuevo privado (0600); recover reutiliza este fichero")
		c.Flags().BoolVar(&f.dryRun, "dry-run", false, "valida y muestra el cuerpo sin autenticación ni envío")
		c.Flags().BoolVar(&f.skeleton, "skeleton", false, "muestra la plantilla JSON sin enviar")
	}
	if op.paginated {
		c.Flags().IntVar(&f.page, "page", 1, "página inicial (1–1000000)")
		c.Flags().IntVar(&f.perPage, "per-page", 50, "tamaño de página (1–100)")
		c.Flags().BoolVar(&f.all, "all", false, "recorre páginas autorizadas y emite NDJSON")
		c.Flags().IntVar(&f.maxPages, "max-pages", 1000, "máximo de peticiones con --all (1–10000)")
	}
	if op.action == "list" {
		c.Flags().StringVar(&f.status, "status", "", "new, assigned, pending, closed, rejected, duplicate")
		c.Flags().StringVar(&f.sort, "sort", "", "id, version, title, status, priority, created_at, updated_at")
		c.Flags().StringVar(&f.direction, "direction", "", "asc, desc")
		for name, values := range map[string][]string{"status": {"new", "assigned", "pending", "closed", "rejected", "duplicate"}, "sort": {"id", "version", "title", "status", "priority", "created_at", "updated_at"}, "direction": {"asc", "desc"}} {
			_ = c.RegisterFlagCompletionFunc(name, func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
				return values, cobra.ShellCompDirectiveNoFileComp
			})
		}
	}
	return c
}

func newCRMServiceTicketRecoverCommand() *cobra.Command {
	var intentFile string
	var skip bool
	c := &cobra.Command{Use: "recover", Short: "Recupera el recibo mediante replay explícito del intent original", Args: UsageArgs(cobra.NoArgs), Long: "Conserva el cuerpo, key, UUID, CAS y headers sellados del envío original. Requiere servidor, credencial, entorno y perfil originales; no acepta overrides ni reintenta automáticamente."}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		if intentFile == "" || intentFile == "-" {
			return apierr.Usagef("recover requiere --intent-file original")
		}
		intent, err := loadCRMServiceTicketIntent(intentFile)
		if err != nil {
			return err
		}
		cc, closeClient, err := newCRMDocumentContext(cmd, intent.APIVersion, intent.ActiveProfile)
		if err != nil {
			return err
		}
		defer closeClient()
		op, request, err := intent.validate(cc)
		if err != nil {
			return reportCRMDocument(cmd, cc, err)
		}
		if err := authorizeCRMServiceTicket(cmd, cc, op, skip); err != nil {
			return err
		}
		return dispatchCRMServiceTicketWrite(cmd, cc, op, intent, request, intentFile)
	}
	c.Flags().StringVar(&intentFile, "intent-file", "", "checkpoint privado original (0600)")
	c.Flags().BoolVar(&skip, "skip-scope-check", false, "omite preflight local; la API revalida autoridad actual")
	return c
}

func authorizeCRMServiceTicket(cmd *cobra.Command, cc *cliContext, op crmServiceTicketOperation, skip bool) error {
	if op.mutating() {
		if err := safety.RequireLive(cc.res.Environment, cc.g.Live); err != nil {
			return err
		}
	}
	if skip {
		return nil
	}
	scopes, err := cc.scopes(cmd.Context())
	if err != nil {
		if cc.errorFormat != output.JSON && !cc.g.Quiet {
			fmt.Fprintln(cmd.ErrOrStderr(), "aviso: preflight de scopes no disponible; la API verificará la autoridad actual")
		}
		return nil
	}
	if !safety.HasScope(scopes, op.scope()) {
		return reportCRMDocument(cmd, cc, apierr.Permf("la credencial no tiene el scope %q", op.scope()))
	}
	return nil
}

func dispatchCRMServiceTicketWrite(cmd *cobra.Command, cc *cliContext, op crmServiceTicketOperation, intent crmServiceTicketIntent, request map[string]any, intentFile string) error {
	wire := crmDocumentIntent(intent)
	resp, err := cc.client.Do(cmd.Context(), intent.Method, intent.Path, []byte(intent.Body), wire.headers())
	if err != nil {
		var api *apierr.APIError
		unconfirmed := !errors.As(err, &api)
		if api != nil {
			code := strings.ToLower(api.Code + " " + api.Subcode)
			unconfirmed = api.StatusCode >= 500 || api.StatusCode == 403 || api.StatusCode == 404 || (api.StatusCode >= 300 && api.StatusCode < 400) || strings.Contains(code, "unconfirmed") || strings.Contains(code, "revoked") || strings.Contains(code, "authorization_changed")
		}
		if unconfirmed {
			return reportCRMDocumentUnconfirmed(cmd, cc, err, "service-tickets", intentFile)
		}
		return reportCRMDocument(cmd, cc, err)
	}
	data, decodeErr := crmDocumentData(resp.Body)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Location") != "" || decodeErr != nil || !strings.Contains(strings.ToLower(resp.ContentType), "json") {
		return reportCRMDocumentUnconfirmed(cmd, cc, &apierr.TransportError{Err: fmt.Errorf("recibo original de ticket ausente o incompatible (request_id=%s)", resp.RequestID)}, "service-tickets", intentFile)
	}
	if err := validateCRMServiceTicketReceipt(op, intent.Args[0], request, data); err != nil {
		return reportCRMDocumentUnconfirmed(cmd, cc, &apierr.TransportError{Err: fmt.Errorf("recibo distinto del UUID/CAS original de ticket (request_id=%s): %w", resp.RequestID, err)}, "service-tickets", intentFile)
	}
	return printCRMServiceTicketBody(cmd.OutOrStdout(), resp.Body, data, cc.format, op)
}

func crmServiceTicketQuery(cmd *cobra.Command, op crmServiceTicketOperation, f *crmServiceTicketFlags) (url.Values, error) {
	q := url.Values{}
	if !op.paginated {
		return q, nil
	}
	if f.page < 1 || f.page > 1000000 || f.perPage < 1 || f.perPage > 100 || f.maxPages < 1 || f.maxPages > 10000 || (!f.all && cmd.Flags().Changed("max-pages")) {
		return nil, apierr.Usagef("page/per-page fuera de rango; --max-pages (1–10000) requiere --all")
	}
	q.Set("page", strconv.Itoa(f.page))
	q.Set("per_page", strconv.Itoa(f.perPage))
	if op.action == "list" {
		for name, value := range map[string]string{"status": f.status, "sort": f.sort, "direction": f.direction} {
			if !cmd.Flags().Changed(name) {
				continue
			}
			valid := false
			switch name {
			case "status":
				valid = crmDocumentEnum(value, "new", "assigned", "pending", "closed", "rejected", "duplicate")
			case "sort":
				valid = crmDocumentEnum(value, "id", "version", "title", "status", "priority", "created_at", "updated_at")
			case "direction":
				valid = crmDocumentEnum(value, "asc", "desc")
			}
			if !valid {
				return nil, apierr.Usagef("filtro nativo %s inválido", name)
			}
			q.Set(name, value)
		}
	}
	return q, nil
}

func crmServiceTicketValidatedRead(op crmServiceTicketOperation, args []string, query url.Values, resp *client.Response) (map[string]any, error) {
	data, err := crmDocumentData(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK || resp.Header.Get("Location") != "" || !strings.Contains(strings.ToLower(resp.ContentType), "json") {
		return nil, &apierr.TransportError{Err: errors.New("respuesta de ticket incompatible con el contrato v1 JSON nativo")}
	}
	if err := validateCRMServiceTicketRead(op, args, query, data); err != nil {
		return nil, &apierr.TransportError{Err: fmt.Errorf("respuesta de ticket incompatible: %w", err)}
	}
	return data, nil
}

func runCRMServiceTicketPages(cmd *cobra.Command, cc *cliContext, op crmServiceTicketOperation, args []string, query url.Values, f *crmServiceTicketFlags) error {
	for fetched := 0; fetched < f.maxPages; fetched++ {
		resp, err := cc.client.Do(cmd.Context(), http.MethodGet, crmDocumentQueryPath(op.pathFor(args), query), nil, crmDocumentHeaders(f.apiVersion, f.activeProfile))
		if err != nil {
			return reportCRMDocument(cmd, cc, err)
		}
		data, err := crmServiceTicketValidatedRead(op, args, query, resp)
		if err != nil {
			return reportCRMDocument(cmd, cc, err)
		}
		items := data["items"].([]any)
		for _, item := range items {
			if err := output.PrintJSON(cmd.OutOrStdout(), item); err != nil {
				return err
			}
		}
		page, _ := crmDocumentInt(data["page"], false)
		perPage, _ := crmDocumentInt(data["per_page"], false)
		total, _ := crmDocumentInt(data["total"], true)
		if page*perPage >= total {
			return nil
		}
		if len(items) == 0 || page == 1000000 {
			return reportCRMDocument(cmd, cc, &apierr.TransportError{Err: errors.New("la página de tickets no avanza; salida parcial")})
		}
		query.Set("page", strconv.FormatInt(page+1, 10))
	}
	return reportCRMDocument(cmd, cc, apierr.Usagef("se alcanzó --max-pages; salida parcial"))
}

func printCRMServiceTicketBody(w io.Writer, body []byte, data map[string]any, format output.Format, op crmServiceTicketOperation) error {
	if format == output.JSON {
		return output.PrintBody(w, body, format)
	}
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	row := func(values ...any) error {
		for i, value := range values {
			if i > 0 {
				if _, err := fmt.Fprint(table, "\t"); err != nil {
					return err
				}
			}
			text := ""
			if value != nil {
				text = strings.Map(func(r rune) rune {
					if r < 32 || r == 127 || (r >= 128 && r <= 159) {
						return ' '
					}
					return r
				}, fmt.Sprint(value))
			}
			if _, err := fmt.Fprint(table, text); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintln(table)
		return err
	}
	if op.action == "timeline" {
		if err := row("VERSION", "ACCIÓN", "ACTOR", "FECHA"); err != nil {
			return err
		}
		for _, item := range data["items"].([]any) {
			o := item.(map[string]any)
			if err := row(o["version"], o["action"], o["actor_id"], o["occurred_at"]); err != nil {
				return err
			}
		}
	} else {
		if err := row("ID", "VERSION", "ESTADO", "TÍTULO"); err != nil {
			return err
		}
		items := []any{data}
		if op.action == "list" {
			items = data["items"].([]any)
		}
		for _, item := range items {
			o := item.(map[string]any)
			if err := row(o["id"], o["version"], o["status"], o["title"]); err != nil {
				return err
			}
		}
	}
	if op.paginated {
		if err := row("PÁGINA", data["page"], "POR PÁGINA", data["per_page"], "TOTAL", data["total"]); err != nil {
			return err
		}
	}
	return table.Flush()
}

func crmServiceTicketManifest() []manifestEntry {
	entries := make([]manifestEntry, 0, len(crmServiceTicketOperations)+1)
	for _, op := range crmServiceTicketOperations {
		entry := manifestEntry{Command: "factuarea crm service-tickets " + op.action, OperationID: op.operationID, Summary: op.summary, Args: append([]string{}, op.args...), Flags: []flagInfo{}, Mutating: op.mutating(), Paginated: op.paginated, RequiredScope: op.scope(), Example: crmServiceTicketExamples[op.action]}
		newCRMServiceTicketCommand(op).Flags().VisitAll(func(flag *pflag.Flag) { entry.Flags = append(entry.Flags, flagInfo{flag.Name, flag.Value.Type()}) })
		required, optional := crmServiceTicketBodyFields(op.action)
		for _, fields := range [][]string{required, optional} {
			for _, name := range fields {
				kind := "string"
				if name == "expected_version" || name == "document_version" {
					kind = "integer"
				}
				if name == "waiting_reason" || name == "customer_message_id" {
					kind = "string|null"
				}
				req := false
				for _, field := range required {
					if field == name {
						req = true
					}
				}
				field := manifestField{Name: name, Type: kind, Kind: kind, Required: req}
				if name == "waiting_reason" {
					field.Enum = []string{"waiting_customer"}
				}
				entry.BodyFields = append(entry.BodyFields, field)
			}
		}
		entries = append(entries, entry)
	}
	return append(entries, manifestEntry{Command: "factuarea crm service-tickets recover", Summary: "Replay explícito del intent original; no endpoint adicional", Args: []string{}, Flags: []flagInfo{{"intent-file", "string"}, {"skip-scope-check", "boolean"}}, Mutating: true, RequiredScope: "customer_service:write"})
}
