package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
)

type serviceLevelOperation struct {
	action, operationID, method, path, summary string
	ticket, paginated                          bool
}

var serviceLevelOperations = []serviceLevelOperation{
	{"status", "getServiceSlaStatus", http.MethodGet, "/v1/crm/service-tickets/{ticket}/sla", "Consulta los tres relojes SLA de un ticket", true, false},
	{"history", "getServiceSlaHistory", http.MethodGet, "/v1/crm/service-tickets/{ticket}/sla/history", "Consulta el historial de ciclos SLA", true, true},
	{"calendars", "listServiceCalendars", http.MethodGet, "/v1/crm/service-calendars", "Lista los calendarios de servicio autorizados", false, true},
	{"configure", "configureServiceSla", http.MethodPut, "/v1/crm/service-tickets/{ticket}/sla", "Configura o revisa el SLA de un ticket con CAS", true, false},
	{"pause", "pauseServiceSla", http.MethodPost, "/v1/crm/service-tickets/{ticket}/sla/pause", "Pausa manualmente los relojes SLA seleccionados", true, false},
	{"resume", "resumeServiceSla", http.MethodPost, "/v1/crm/service-tickets/{ticket}/sla/resume", "Reanuda una pausa manual SLA por su motivo", true, false},
	{"update-calendar", "updateServiceCalendar", http.MethodPut, "/v1/crm/service-calendars", "Crea o revisa un calendario de servicio con CAS", false, false},
}

func (op serviceLevelOperation) mutating() bool { return op.method != http.MethodGet }

func (op serviceLevelOperation) scopes() []string {
	if op.mutating() {
		return []string{"service_level:write", "crm:write"}
	}
	return []string{"service_level:read", "crm:read"}
}

// This module consumes the seven owner routes directly. It does not regenerate
// the published spec or advertise a receipt endpoint that the owner lacks.
func registerServiceLevelCommands(root *cobra.Command) {
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
	group := serviceLevelGroup("service-level", "Calendarios de servicio y relojes SLA de tickets")
	for _, op := range serviceLevelOperations {
		group.AddCommand(newServiceLevelCommand(op))
	}
	crm.AddCommand(group)
}

func serviceLevelGroup(use, summary string) *cobra.Command {
	return &cobra.Command{Use: use, Short: summary, Args: groupArgs, SuggestionsMinimumDistance: 2,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
}

func newServiceLevelCommand(op serviceLevelOperation) *cobra.Command {
	var data, dataFile, idempotencyKey string
	var dryRun, skeleton, skipScopeCheck, all bool
	page, perPage, maxPages := 1, 50, 100
	use, argCount := op.action, 0
	if op.ticket {
		use += " <ticket>"
		argCount = 1
	}
	long := op.summary + "\n\nCompany, actor y entorno proceden de la credencial del perfil. Los identificadores son UUIDv7."
	if op.mutating() {
		long += "\nEl cuerpo es un objeto JSON cerrado: todos los campos de la plantilla son obligatorios, incluidos los null. Las identidades nuevas se reservan en el servidor.\nUsa la misma --idempotency-key y el mismo cuerpo para consultar el resultado original mediante replay explícito. No hay reintentos automáticos; un timeout o recibo ausente deja el resultado sin confirmar.\nEjemplo de body (--data):\n  " + serviceLevelExamples[op.action]
	}
	if op.paginated {
		long += "\nLa API usa page/per_page. --all recorre páginas y emite NDJSON, con un máximo explícito; puede emitir filas antes de un error y no es un snapshot transaccional."
	}
	c := &cobra.Command{Use: use, Short: op.summary, Long: long, Args: UsageArgs(cobra.ExactArgs(argCount))}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		path := op.path
		if op.ticket {
			if !serviceLevelUUID.MatchString(args[0]) {
				return apierr.Usagef("ticket debe ser un UUIDv7")
			}
			path = strings.ReplaceAll(path, "{ticket}", strings.ToLower(args[0]))
		}
		var body []byte
		if op.mutating() {
			if (data != "" && dataFile != "") || (skeleton && (data != "" || dataFile != "" || dryRun)) {
				return apierr.Usagef("elige una sola fuente: --data, --data-file o --skeleton")
			}
			if skeleton {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), serviceLevelExamples[op.action])
				return err
			}
			var err error
			if dataFile != "" {
				body, err = readInputFile(dataFile)
			} else if data != "" {
				body, err = readDataArg(data, cmd.InOrStdin())
			} else {
				return apierr.Usagef("declara el cuerpo JSON con --data o --data-file; --skeleton muestra la plantilla")
			}
			if err != nil {
				return err
			}
			if err := validateServiceLevelBody(op.action, body); err != nil {
				return err
			}
			if dryRun {
				return output.PrintBody(cmd.OutOrStdout(), body, output.JSON)
			}
			if err := validateServiceLevelIdempotencyKey(idempotencyKey); err != nil {
				return err
			}
		}
		if op.paginated && (page < 1 || page > 1000000 || perPage < 1 || perPage > 100 || maxPages < 1 || maxPages > 10000) {
			return apierr.Usagef("page debe estar entre 1 y 1000000, per-page entre 1 y 100 y max-pages entre 1 y 10000")
		}
		if op.paginated && cmd.Flags().Changed("max-pages") && !all {
			return apierr.Usagef("--max-pages requiere --all")
		}
		g := globalsFrom(cmd)
		cc, err := newCLIContext(g, "")
		if err != nil {
			return err
		}
		// Keep the host's profile, environment and credential resolution. Only
		// this module disables application retries, redirects and replay of a
		// buffered body by the HTTP transport; the shared client is unchanged.
		opts, err := baseURLClientOptions(g.AllowInsecureTransport)
		if err != nil {
			return err
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.DisableKeepAlives = true
		hc := &http.Client{Timeout: 60 * time.Second, Transport: serviceLevelSingleDispatch{transport},
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
		opts = append(opts, client.WithMaxRetries(0), client.WithHTTPClient(hc))
		cc.client = client.New(cc.res.APIKey, opts...)
		defer transport.CloseIdleConnections()
		if op.mutating() {
			if err := safety.RequireLive(cc.res.Environment, g.Live); err != nil {
				return err
			}
		}
		if !skipScopeCheck {
			scopes, err := cc.scopes(cmd.Context())
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "aviso: no pude verificar scopes (%v); la API verificará la autoridad actual\n", err)
			} else {
				for _, scope := range op.scopes() {
					if !safety.HasScope(scopes, scope) {
						return serviceLevelReport(cmd, cc, apierr.Permf("la API key no tiene el scope %q requerido por esta operación", scope))
					}
				}
			}
		}
		if all {
			return runServiceLevelPages(cmd, cc, path, page, perPage, maxPages)
		}
		if op.paginated {
			path += "?" + serviceLevelPageQuery(page, perPage)
		}
		var headers map[string]string
		if op.mutating() {
			headers = map[string]string{"Idempotency-Key": idempotencyKey}
		}
		resp, err := cc.client.Do(cmd.Context(), op.method, path, body, headers)
		if err != nil {
			var transportError *apierr.TransportError
			if op.mutating() && errors.As(err, &transportError) {
				err = &apierr.TransportError{Err: fmt.Errorf("resultado SLA sin confirmar; conserva el cuerpo y la clave original para reconciliarlo mediante replay explícito: %w", err)}
			}
			return serviceLevelReport(cmd, cc, err)
		}
		if op.mutating() {
			if err := validateServiceLevelReceipt(resp.Body); err != nil {
				return serviceLevelReport(cmd, cc, err)
			}
		}
		if g.Verbose && resp.RequestID != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "request_id: %s\n", resp.RequestID)
		}
		return printServiceLevelBody(cmd.OutOrStdout(), resp.Body, cc.format, op)
	}
	c.Flags().BoolVar(&skipScopeCheck, "skip-scope-check", false, "omite la comprobación local de scopes; la API conserva su autorización")
	if op.mutating() {
		c.Flags().StringVarP(&data, "data", "d", "", "cuerpo JSON (@fichero o - para stdin)")
		c.Flags().StringVar(&dataFile, "data-file", "", "fichero con el cuerpo JSON")
		c.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "clave ASCII original obligatoria (1–255 caracteres); se conserva sin regenerar")
		c.Flags().BoolVar(&dryRun, "dry-run", false, "valida e imprime el cuerpo sin llamar a la API")
		c.Flags().BoolVar(&skeleton, "skeleton", false, "imprime la plantilla JSON sin llamar a la API")
	}
	if op.paginated {
		c.Flags().IntVar(&page, "page", 1, "página inicial (1–1000000)")
		c.Flags().IntVar(&perPage, "per-page", 50, "elementos por página (1–100), enviado como per_page")
		c.Flags().BoolVar(&all, "all", false, "recorre páginas con salida NDJSON; puede terminar con salida parcial")
		c.Flags().IntVar(&maxPages, "max-pages", 100, "máximo de páginas con --all (1–10000)")
	}
	return c
}

type serviceLevelSingleDispatch struct{ transport http.RoundTripper }

func (t serviceLevelSingleDispatch) RoundTrip(req *http.Request) (*http.Response, error) {
	single := req.Clone(req.Context())
	single.GetBody = nil
	return t.transport.RoundTrip(single)
}

func serviceLevelReport(cmd *cobra.Command, cc *cliContext, err error) error {
	output.PrintError(cmd.ErrOrStderr(), err, cc.errorFormat)
	return &AlreadyReported{Err: err}
}

func serviceLevelPageQuery(page, perPage int) string {
	return url.Values{"page": {strconv.Itoa(page)}, "per_page": {strconv.Itoa(perPage)}}.Encode()
}

func runServiceLevelPages(cmd *cobra.Command, cc *cliContext, path string, page, perPage, maxPages int) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	for fetched := 0; fetched < maxPages; fetched++ {
		resp, err := cc.client.Do(cmd.Context(), http.MethodGet, path+"?"+serviceLevelPageQuery(page, perPage), nil, nil)
		if err != nil {
			return serviceLevelReport(cmd, cc, err)
		}
		result, err := decodeServiceLevelPage(resp.Body)
		if err != nil || result.Page != page || result.PerPage != perPage {
			return serviceLevelReport(cmd, cc, &apierr.TransportError{Err: errors.New("la API devolvió una página SLA incompatible; no se continúa la iteración")})
		}
		for _, item := range result.Items {
			if err := enc.Encode(item); err != nil {
				return err
			}
		}
		if int64(page)*int64(perPage) >= result.Total {
			return nil
		}
		if len(result.Items) == 0 || page == 1000000 {
			return serviceLevelReport(cmd, cc, &apierr.TransportError{Err: errors.New("la paginación SLA no avanzó; salida parcial")})
		}
		page++
	}
	return apierr.Usagef("se alcanzó --max-pages; la salida es parcial. Continúa con --page=%d o aumenta el límite explícitamente", page)
}

func serviceLevelManifest() []manifestEntry {
	entries := make([]manifestEntry, 0, len(serviceLevelOperations))
	for _, op := range serviceLevelOperations {
		entry := manifestEntry{Command: "factuarea crm service-level " + op.action, OperationID: op.operationID, Summary: op.summary,
			Args: []string{}, Flags: []flagInfo{{Name: "skip-scope-check", Type: "boolean"}}, Mutating: op.mutating(), Paginated: op.paginated,
			RequiredScope: op.scopes()[0], Example: serviceLevelExamples[op.action]}
		if op.ticket {
			entry.Args = []string{"ticket"}
		}
		if op.paginated {
			entry.Flags = append(entry.Flags, flagInfo{"page", "integer"}, flagInfo{"per-page", "integer"}, flagInfo{"all", "boolean"}, flagInfo{"max-pages", "integer"})
		}
		if op.mutating() {
			entry.Flags = append(entry.Flags, flagInfo{"data", "string"}, flagInfo{"data-file", "string"}, flagInfo{"idempotency-key", "string"}, flagInfo{"dry-run", "boolean"}, flagInfo{"skeleton", "boolean"})
			entry.FullReplace = op.method == http.MethodPut
			entry.BodyHasObjects = op.action == "update-calendar"
			fields := serviceLevelSchema(op.action).properties
			names := make([]string, 0, len(fields))
			for name := range fields {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				field := fields[name]
				kind := field.kind
				if field.nullable {
					kind += "|null"
				}
				entry.BodyFields = append(entry.BodyFields, manifestField{Name: name, Type: kind, Kind: field.kind, Required: true})
			}
		}
		entries = append(entries, entry)
	}
	return entries
}
