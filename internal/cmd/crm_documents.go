package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/client"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/factuarea/factuarea-cli/internal/safety"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type crmDocumentOperation struct {
	group, action, operationID, method, path, summary string
	args                                              []string
	pagination                                        string
}

var crmDocumentOperations = []crmDocumentOperation{
	{"documents", "list", "crm_documents.list", "GET", "/v1/crm/documents", "Lista la biblioteca privada autorizada", nil, "page"},
	{"documents", "get", "crm_documents.get", "GET", "/v1/crm/documents/{document}", "Consulta un documento con los campos autorizados", []string{"document"}, ""},
	{"documents", "upload", "crm_documents.upload", "POST", "/v1/crm/documents", "Registra una referencia de ingestión nativa acreditada", nil, ""},
	{"documents", "replace-version", "crm_documents.replace_version", "POST", "/v1/crm/documents/{document}/versions", "Añade una versión nativa con el CAS original", []string{"document"}, ""},
	{"documents", "share", "crm_documents.share", "POST", "/v1/crm/documents/{document}/shares", "Comparte un documento con un usuario o equipo", []string{"document"}, ""},
	{"documents", "revoke-share", "crm_documents.revoke_share", "POST", "/v1/crm/documents/{document}/shares/revoke", "Revoca el grant indicado con CAS", []string{"document"}, ""},
	{"documents", "archive", "crm_documents.archive", "POST", "/v1/crm/documents/{document}/archive", "Archiva el documento con CAS", []string{"document"}, ""},
	{"documents", "download", "crm_documents.download", "GET", "/v1/crm/documents/{document}/download", "Descarga bytes privados mediante autorización nativa", []string{"document"}, ""},
	{"opportunity-documents", "list", "crm_opportunitydocuments.list", "GET", "/v1/crm/opportunitydocuments/{opportunity}/documents", "Lista las asociaciones canónicas de una oportunidad", []string{"opportunity"}, "cursor"},
	{"opportunity-documents", "candidates", "crm_opportunitydocuments.candidates", "GET", "/v1/crm/opportunities/{opportunity}/documents/candidates", "Consulta candidatos autorizados del propietario nativo", []string{"opportunity"}, "cursor"},
	{"opportunity-documents", "create-quote", "crm_opportunitydocuments.create_quote", "POST", "/v1/crm/opportunitydocuments/{opportunity}/quotes", "Crea y vincula un presupuesto prefiscal nativo", []string{"opportunity"}, ""},
	{"opportunity-documents", "link", "crm_opportunitydocuments.link_document", "POST", "/v1/crm/opportunitydocuments/{opportunity}/documents", "Vincula un documento canónico existente", []string{"opportunity"}, ""},
	{"opportunity-documents", "unlink", "crm_opportunitydocuments.unlink_document", "DELETE", "/v1/crm/opportunitydocuments/{opportunity}/documents/{link}", "Quita una asociación con su CAS original", []string{"opportunity", "link"}, ""},
	{"opportunity-documents", "financial-summary", "crm_opportunitydocuments.financial_summary", "GET", "/v1/crm/opportunitydocuments/{opportunity}/financial-summary", "Consulta importes exactos separados por divisa", []string{"opportunity"}, ""},
}

var crmDocumentExamples = map[string]string{
	"upload":          `{"title":"Oferta","producer_reference_id":"01900000-0000-7000-8000-000000000001","claimed_mime":"application/pdf"}`,
	"replace-version": `{"expected_version":1,"producer_reference_id":"01900000-0000-7000-8000-000000000001","claimed_mime":"application/pdf"}`,
	"share":           `{"expected_version":1,"grant_type":"user","grantee_id":"01900000-0000-4000-8000-000000000001"}`,
	"revoke-share":    `{"expected_version":1,"share_id":"01900000-0000-7000-8000-000000000001"}`,
	"archive":         `{"expected_version":1}`,
	"create-quote":    `{"draft":{"date":"2026-10-09","recipient":{"kind":"unassigned"}}}`,
	"link":            `{"document_type":"quote","document_id":"01900000-0000-4000-8000-000000000001","is_primary":false}`,
	"unlink":          `{"expected_version":1}`,
}

func (op crmDocumentOperation) mutating() bool { return op.method != http.MethodGet }
func (op crmDocumentOperation) scope() string {
	resource := "crm_documents"
	if op.group == "opportunity-documents" {
		resource = "crm_opportunitydocuments"
	}
	if op.mutating() {
		return resource + ":write"
	}
	return resource + ":read"
}
func (op crmDocumentOperation) status() int {
	if op.action == "upload" || op.action == "create-quote" || op.action == "link" {
		return http.StatusCreated
	}
	return http.StatusOK
}
func (op crmDocumentOperation) confirmation() bool {
	return op.action == "archive" || op.action == "revoke-share" || op.action == "unlink"
}
func (op crmDocumentOperation) validateArgs(args []string) error {
	if len(args) != len(op.args) {
		return apierr.Usagef("número de identificadores incompatible con %s", op.action)
	}
	for index, arg := range args {
		if !crmDocumentID(arg, false) {
			return apierr.Usagef("%s debe ser un UUIDv7", op.args[index])
		}
	}
	return nil
}
func (op crmDocumentOperation) pathFor(args []string) string {
	path := op.path
	for index, arg := range args {
		path = strings.ReplaceAll(path, "{"+op.args[index]+"}", arg)
	}
	return path
}

func registerCRMDocumentCommands(root *cobra.Command) {
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
	for _, name := range []string{"documents", "opportunity-documents"} {
		group := serviceLevelGroup(name, "Documentos CRM sobre las APIs v1 JSON nativas")
		for _, op := range crmDocumentOperations {
			if op.group == name {
				group.AddCommand(newCRMDocumentCommand(op))
			}
		}
		group.AddCommand(newCRMDocumentRecoverCommand(name))
		crm.AddCommand(group)
	}
}

type crmDocumentFlags struct {
	data, dataFile, key, intentFile, confirm, apiVersion, activeProfile, outputFile string
	search, sort, cursor, filters, documentType, statusFilter, direction            string
	dryRun, skeleton, skipScopeCheck, all, archived, isPrimary                      bool
	page, perPage, limit, maxPages                                                  int
	documentVersion                                                                 int64
}

func newCRMDocumentCommand(op crmDocumentOperation) *cobra.Command {
	f := &crmDocumentFlags{page: 1, perPage: 25, limit: 25, maxPages: 1000}
	use := op.action
	for _, arg := range op.args {
		use += " <" + arg + ">"
	}
	long := op.summary + "\n\nCompany, actor y entorno proceden de la credencial nativa (--profile / FACTUAREA_API_KEY)."
	if op.mutating() {
		long += "\nCuerpo JSON cerrado; conserva omitido/null y UUID/CAS nativos. Antes del envío se guarda --intent-file privado (0600), sin sobrescribirlo ni copiar la credencial. --idempotency-key es obligatoria y se conserva sin regenerar.\nUn resultado sin confirmar exige recover --intent-file con la credencial, servidor y perfil originales. El replay es explícito, sin reintentos automáticos ni consulta de una revisión nueva.\nPlantilla:\n  " + crmDocumentExamples[op.action]
	}
	if op.pagination != "" {
		long += "\n--all emite NDJSON y recorre páginas/cursor hasta --max-pages; la salida puede ser parcial si falla un paso y no es un snapshot transaccional."
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
				_, err := fmt.Fprintln(cmd.OutOrStdout(), crmDocumentExamples[op.action])
				return err
			}
			var err error
			if f.dataFile != "" {
				body, err = readInputFile(f.dataFile)
			} else if f.data != "" {
				body, err = readDataArg(f.data, cmd.InOrStdin())
			} else {
				return apierr.Usagef("declara el cuerpo con --data o --data-file; --skeleton muestra la plantilla")
			}
			if err != nil {
				return err
			}
			request, err = validateCRMDocumentBody(op, body)
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
				return apierr.Usagef("--intent-file es obligatorio antes de enviar una mutación")
			}
			if err := confirmCRMDocument(cmd, op, args, f.confirm); err != nil {
				return err
			}
		}
		query, err := crmDocumentQuery(cmd, op, f)
		if err != nil {
			return err
		}
		if op.action == "download" && (f.outputFile == "" || f.outputFile == "-") {
			return apierr.Usagef("download requiere --output con un fichero nuevo privado; los bytes no se mezclan con JSON")
		}
		cc, closeClient, err := newCRMDocumentContext(cmd, f.apiVersion, f.activeProfile)
		if err != nil {
			return err
		}
		defer closeClient()
		if err := authorizeCRMDocument(cmd, cc, op, f.skipScopeCheck, f.apiVersion, f.activeProfile); err != nil {
			return err
		}
		if op.mutating() {
			intent := newCRMDocumentIntent(op, args, body, f.key, f.apiVersion, f.activeProfile, cc)
			if err := saveCRMDocumentIntent(f.intentFile, intent); err != nil {
				return err
			}
			return dispatchCRMDocumentWrite(cmd, cc, op, intent, request, f.intentFile)
		}
		if f.all {
			return runCRMDocumentPages(cmd, cc, op, args, query, f)
		}
		resp, err := cc.client.Do(cmd.Context(), op.method, crmDocumentQueryPath(op.pathFor(args), query), nil, crmDocumentHeaders(f.apiVersion, f.activeProfile))
		if err != nil {
			return reportCRMDocument(cmd, cc, err)
		}
		if op.action == "download" {
			if resp.StatusCode != http.StatusOK || resp.Header.Get("Location") != "" || resp.Header.Get("Content-Disposition") == "" {
				return reportCRMDocument(cmd, cc, &apierr.TransportError{Err: errors.New("download no devolvió bytes privados nativos; no se guardó un enlace ni una respuesta parcial")})
			}
			if err := crmDocumentWritePrivate(f.outputFile, resp.Body); err != nil {
				return err
			}
			return output.PrintJSON(cmd.OutOrStdout(), map[string]any{"output": f.outputFile, "size_bytes": len(resp.Body), "content_type": resp.ContentType, "content_disposition": resp.Header.Get("Content-Disposition"), "request_id": resp.RequestID})
		}
		data, err := crmDocumentValidatedRead(op, args, query, resp)
		if err != nil {
			return reportCRMDocument(cmd, cc, err)
		}
		return printCRMDocumentBody(cmd.OutOrStdout(), resp.Body, data, cc.format, op)
	}
	addCRMDocumentCommonFlags(c, f)
	if op.mutating() {
		c.Flags().StringVarP(&f.data, "data", "d", "", "cuerpo JSON (@fichero o - para stdin)")
		c.Flags().StringVar(&f.dataFile, "data-file", "", "fichero con cuerpo JSON original")
		c.Flags().StringVar(&f.key, "idempotency-key", "", "clave ASCII original obligatoria (1–255 caracteres)")
		c.Flags().StringVar(&f.intentFile, "intent-file", "", "checkpoint privado nuevo obligatorio antes del envío; recover reutiliza este fichero")
		c.Flags().BoolVar(&f.dryRun, "dry-run", false, "valida y muestra el cuerpo sin autenticación ni envío")
		c.Flags().BoolVar(&f.skeleton, "skeleton", false, "muestra una plantilla JSON sin enviar")
		if op.confirmation() {
			c.Flags().StringVar(&f.confirm, "confirm", "", "confirma la retirada/archivo con el UUID exacto del recurso")
		}
	}
	if op.pagination != "" {
		c.Flags().BoolVar(&f.all, "all", false, "recorre páginas/cursor autorizado y emite NDJSON")
		c.Flags().IntVar(&f.maxPages, "max-pages", 1000, "máximo explícito de peticiones con --all (1–10000)")
		c.Flags().StringVar(&f.search, "search", "", "búsqueda nativa")
		if op.pagination == "page" {
			c.Flags().IntVar(&f.page, "page", 1, "página inicial (1–1000000)")
			c.Flags().IntVar(&f.perPage, "per-page", 25, "tamaño de página (1–100)")
			c.Flags().StringVar(&f.sort, "sort", "", "orden nativo: id, title, version")
			c.Flags().BoolVar(&f.archived, "archived", false, "filtro de archivo explícito true/false")
		} else {
			c.Flags().IntVar(&f.limit, "limit", 25, "tamaño de página (1–100)")
			c.Flags().StringVar(&f.cursor, "cursor", "", "cursor opaco original de la API")
			c.Flags().StringVar(&f.documentType, "document-type", "", "propietario: quote, invoice, contract, recurring_schedule")
			if op.action == "list" {
				// List has typed filters, not a search field in the native API.
				c.Flags().MarkHidden("search")
				c.Flags().StringVar(&f.filters, "filters", "", "objeto JSON cerrado de filtros; alternativa a los flags tipados")
				c.Flags().StringVar(&f.statusFilter, "status", "", "pending, linked, conflict")
				c.Flags().BoolVar(&f.isPrimary, "is-primary", false, "filtro booleano explícito")
				c.Flags().StringVar(&f.sort, "sort", "", "id, version, document_type, document_id, status, is_primary")
				c.Flags().StringVar(&f.direction, "direction", "", "asc, desc")
			}
		}
	}
	if op.action == "download" {
		c.Flags().Int64Var(&f.documentVersion, "document-version", 0, "versión nativa positiva; omitida usa la autorizada actual")
		c.Flags().StringVarP(&f.outputFile, "output", "o", "", "fichero nuevo privado (0600) para bytes nativos")
	}
	if c.Flags().Lookup("document-type") != nil {
		_ = c.RegisterFlagCompletionFunc("document-type", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			return []string{"quote", "invoice", "contract", "recurring_schedule"}, cobra.ShellCompDirectiveNoFileComp
		})
	}
	return c
}

func addCRMDocumentCommonFlags(c *cobra.Command, f *crmDocumentFlags) {
	c.Flags().BoolVar(&f.skipScopeCheck, "skip-scope-check", false, "omite preflight local; el servidor revalida autoridad efectiva")
	c.Flags().StringVar(&f.apiVersion, "api-version", "", "Factuarea-Version original (YYYY-MM-DD)")
	c.Flags().StringVar(&f.activeProfile, "active-profile", "", "X-Active-Profile nativo; su autorización corresponde al servidor")
}

func newCRMDocumentRecoverCommand(group string) *cobra.Command {
	var intentFile, confirm string
	var skipScopeCheck bool
	c := &cobra.Command{Use: "recover", Short: "Recupera el recibo original mediante replay explícito del intent privado", Args: UsageArgs(cobra.NoArgs),
		Long: "Lee el checkpoint privado sellado y repite exactamente su operación, cuerpo, clave y headers originales. Requiere la misma credencial, servidor, entorno y perfil. No acepta cambios de CAS, body, key o headers ni inventa un endpoint de recibos."}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		if intentFile == "" || intentFile == "-" {
			return apierr.Usagef("recover requiere --intent-file original")
		}
		intent, err := loadCRMDocumentIntent(intentFile)
		if err != nil {
			return err
		}
		cc, closeClient, err := newCRMDocumentContext(cmd, intent.APIVersion, intent.ActiveProfile)
		if err != nil {
			return err
		}
		defer closeClient()
		op, request, err := intent.validate(group, cc)
		if err != nil {
			return reportCRMDocument(cmd, cc, err)
		}
		if err := confirmCRMDocument(cmd, op, intent.Args, confirm); err != nil {
			return err
		}
		if err := authorizeCRMDocument(cmd, cc, op, skipScopeCheck, intent.APIVersion, intent.ActiveProfile); err != nil {
			return err
		}
		return dispatchCRMDocumentWrite(cmd, cc, op, intent, request, intentFile)
	}
	c.Flags().StringVar(&intentFile, "intent-file", "", "checkpoint privado original (0600)")
	c.Flags().StringVar(&confirm, "confirm", "", "UUID exacto del recurso para una retirada/archivo original")
	c.Flags().BoolVar(&skipScopeCheck, "skip-scope-check", false, "omite preflight local; la API conserva autorización actual")
	return c
}

func confirmCRMDocument(cmd *cobra.Command, op crmDocumentOperation, args []string, flag string) error {
	if !op.confirmation() {
		return nil
	}
	target := args[len(args)-1]
	return safety.Confirm(target, flag, output.IsTTY(os.Stdin), globalsFrom(cmd).NoInput, func(prompt string) (string, error) {
		fmt.Fprint(cmd.ErrOrStderr(), prompt)
		return bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	})
}

// A private transport preserves the host client/auth while preventing automatic
// redirects or buffered write replays. Reading the body here also catches a
// truncated transfer that the shared client currently discards as a read error.
type crmDocumentSingleDispatch struct {
	transport http.RoundTripper
	headers   map[string]string
}

func (t crmDocumentSingleDispatch) RoundTrip(req *http.Request) (*http.Response, error) {
	original := req.Clone(req.Context())
	original.GetBody = nil
	for name, value := range t.headers {
		original.Header.Set(name, value)
	}
	resp, err := t.transport.RoundTrip(original)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("respuesta de documentos incompleta (request_id=%s): %w", resp.Header.Get("X-Request-Id"), readErr)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

func newCRMDocumentContext(cmd *cobra.Command, version, profile string) (*cliContext, func(), error) {
	g := globalsFrom(cmd)
	cc, err := newCLIContext(g, "")
	if err != nil {
		return nil, nil, err
	}
	opts, err := baseURLClientOptions(g.AllowInsecureTransport)
	if err != nil {
		return nil, nil, err
	}
	base, err := url.Parse(crmDocumentBaseURL())
	if err != nil || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return nil, nil, apierr.Usagef("FACTUAREA_BASE_URL debe conservar un servidor HTTP sin credenciales, query ni fragmento")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	hc := &http.Client{Timeout: 60 * time.Second, Transport: crmDocumentSingleDispatch{transport, crmDocumentHeaders(version, profile)},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	opts = append(opts, client.WithMaxRetries(0), client.WithHTTPClient(hc))
	cc.client = client.New(cc.res.APIKey, opts...)
	return cc, transport.CloseIdleConnections, nil
}

func authorizeCRMDocument(cmd *cobra.Command, cc *cliContext, op crmDocumentOperation, skip bool, version, profile string) error {
	if op.mutating() {
		if err := safety.RequireLive(cc.res.Environment, cc.g.Live); err != nil {
			return err
		}
	}
	if skip {
		return nil
	}
	// /account is native and uses the same managed-profile/version headers.
	// A failed optional preflight grants nothing: the target API still authorizes
	// the actual operation. Do not print transport URLs containing credentials.
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

func dispatchCRMDocumentWrite(cmd *cobra.Command, cc *cliContext, op crmDocumentOperation, intent crmDocumentIntent, request map[string]any, intentFile string) error {
	resp, err := cc.client.Do(cmd.Context(), intent.Method, intent.Path, []byte(intent.Body), intent.headers())
	if err != nil {
		var api *apierr.APIError
		unconfirmed := !errors.As(err, &api) || api.StatusCode >= 500 || api.StatusCode == 403 || api.StatusCode == 404 || (api.StatusCode >= 300 && api.StatusCode < 400) || api.Code == "write_unconfirmed" || api.Code == "effect_unconfirmed"
		if unconfirmed {
			return reportCRMDocumentUnconfirmed(cmd, cc, err, op.group, intentFile)
		}
		return reportCRMDocument(cmd, cc, err)
	}
	data, decodeErr := crmDocumentData(resp.Body)
	if resp.StatusCode != op.status() || resp.Header.Get("Location") != "" || decodeErr != nil || !strings.Contains(strings.ToLower(resp.ContentType), "json") {
		return reportCRMDocumentUnconfirmed(cmd, cc, &apierr.TransportError{Err: fmt.Errorf("recibo original ausente o respuesta incompatible (request_id=%s)", resp.RequestID)}, op.group, intentFile)
	}
	if err := validateCRMDocumentReceipt(op, intent.Args, request, data); err != nil {
		return reportCRMDocumentUnconfirmed(cmd, cc, &apierr.TransportError{Err: fmt.Errorf("recibo distinto del intent original (request_id=%s): %w", resp.RequestID, err)}, op.group, intentFile)
	}
	return printCRMDocumentBody(cmd.OutOrStdout(), resp.Body, data, cc.format, op)
}

func reportCRMDocument(cmd *cobra.Command, cc *cliContext, err error) error {
	output.PrintError(cmd.ErrOrStderr(), err, cc.errorFormat)
	return &AlreadyReported{Err: err}
}

func reportCRMDocumentUnconfirmed(cmd *cobra.Command, cc *cliContext, err error, group, path string) error {
	if cc.errorFormat == output.JSON {
		var encoded bytes.Buffer
		output.PrintError(&encoded, err, output.JSON)
		payload, decodeErr := crmDocumentJSON(encoded.Bytes())
		if decodeErr != nil {
			payload = map[string]any{"error": map[string]any{"message": "resultado de documentos sin confirmar"}}
		}
		payload["unconfirmed"] = true
		payload["intent_file"] = path
		payload["recovery_command"] = crmDocumentRecoveryHint(group, path, cc.res.Profile, cc.res.Environment == "live")
		_ = output.PrintJSON(cmd.ErrOrStderr(), payload)
	} else {
		output.PrintError(cmd.ErrOrStderr(), err, cc.errorFormat)
		fmt.Fprintf(cmd.ErrOrStderr(), "Resultado sin confirmar. Conserva el intent original: %s\n", crmDocumentRecoveryHint(group, path, cc.res.Profile, cc.res.Environment == "live"))
	}
	return &AlreadyReported{Err: err}
}

func crmDocumentQuery(cmd *cobra.Command, op crmDocumentOperation, f *crmDocumentFlags) (url.Values, error) {
	q := url.Values{}
	if op.pagination != "" {
		if f.maxPages < 1 || f.maxPages > 10000 || (!f.all && cmd.Flags().Changed("max-pages")) {
			return nil, apierr.Usagef("--max-pages requiere --all y un límite 1–10000")
		}
		if op.pagination == "page" {
			if f.page < 1 || f.page > 1000000 || f.perPage < 1 || f.perPage > 100 || len(f.search) > 255 || (f.sort != "" && !crmDocumentEnum(f.sort, "id", "title", "version")) {
				return nil, apierr.Usagef("page/per-page/search/sort no cumplen el contrato de biblioteca")
			}
			q.Set("page", strconv.Itoa(f.page))
			q.Set("per_page", strconv.Itoa(f.perPage))
			if cmd.Flags().Changed("search") {
				q.Set("search", f.search)
			}
			if f.sort != "" {
				q.Set("sort", f.sort)
			}
			if cmd.Flags().Changed("archived") {
				q.Set("archived", strconv.FormatBool(f.archived))
			}
		} else {
			cursorMax := 8192
			if op.action == "candidates" {
				cursorMax = 32768
			}
			if f.limit < 1 || f.limit > 100 || len(f.cursor) > cursorMax || (cmd.Flags().Changed("cursor") && strings.TrimSpace(f.cursor) == "") {
				return nil, apierr.Usagef("limit debe ser 1–100; cursor debe conservar su valor opaco nativo")
			}
			q.Set("limit", strconv.Itoa(f.limit))
			if f.cursor != "" {
				q.Set("cursor", f.cursor)
			}
			if op.action == "candidates" {
				if !crmDocumentType(f.documentType) || len(f.search) > 100 {
					return nil, apierr.Usagef("candidates requiere --document-type nativo y search de hasta 100 bytes")
				}
				q.Set("document_type", f.documentType)
				q.Set("search", f.search)
			} else {
				if cmd.Flags().Changed("search") {
					return nil, apierr.Usagef("list de asociaciones usa --filters; no admite search")
				}
				filters := map[string]any{}
				if f.filters != "" {
					var err error
					filters, err = crmDocumentJSON([]byte(f.filters))
					if err != nil {
						return nil, err
					}
				}
				for _, name := range []string{"document-type", "status", "is-primary", "sort", "direction"} {
					if cmd.Flags().Changed(name) {
						if f.filters != "" {
							return nil, apierr.Usagef("elige --filters o los flags de filtro tipados")
						}
						var value any
						switch name {
						case "document-type":
							value = f.documentType
						case "status":
							value = f.statusFilter
						case "is-primary":
							value = f.isPrimary
						case "sort":
							value = f.sort
						case "direction":
							value = f.direction
						}
						filters[strings.ReplaceAll(name, "-", "_")] = value
					}
				}
				if _, err := crmDocumentRecord(filters, nil, []string{"document_type", "status", "is_primary", "sort", "direction"}); err != nil {
					return nil, err
				}
				for key, value := range filters {
					valid := false
					switch key {
					case "document_type":
						valid = crmDocumentType(value)
					case "status":
						valid = crmDocumentEnum(value, "pending", "linked", "conflict")
					case "is_primary":
						_, valid = value.(bool)
					case "sort":
						valid = crmDocumentEnum(value, "id", "version", "document_type", "document_id", "status", "is_primary")
					case "direction":
						valid = crmDocumentEnum(value, "asc", "desc")
					}
					if !valid {
						return nil, apierr.Usagef("filtro tipado inválido: %s", key)
					}
				}
				body, _ := json.Marshal(filters)
				q.Set("filters", string(body))
			}
		}
	}
	if op.action == "download" && cmd.Flags().Changed("document-version") {
		if f.documentVersion < 1 || f.documentVersion == int64(^uint64(0)>>1) {
			return nil, apierr.Usagef("document-version debe ser un entero positivo en rango nativo")
		}
		q.Set("document_version", strconv.FormatInt(f.documentVersion, 10))
	}
	return q, nil
}

func crmDocumentQueryPath(path string, query url.Values) string {
	if len(query) == 0 {
		return path
	}
	return path + "?" + query.Encode()
}

func crmDocumentValidatedRead(op crmDocumentOperation, args []string, query url.Values, resp *client.Response) (map[string]any, error) {
	data, err := crmDocumentData(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK || resp.Header.Get("Location") != "" || !strings.Contains(strings.ToLower(resp.ContentType), "json") {
		return nil, &apierr.TransportError{Err: errors.New("respuesta de documentos incompatible con el contrato v1 JSON nativo")}
	}
	if err := validateCRMDocumentRead(op, args, data, query); err != nil {
		return nil, &apierr.TransportError{Err: fmt.Errorf("respuesta de documentos incompatible: %w", err)}
	}
	return data, nil
}

func runCRMDocumentPages(cmd *cobra.Command, cc *cliContext, op crmDocumentOperation, args []string, query url.Values, f *crmDocumentFlags) error {
	seen := map[string]bool{}
	if f.cursor != "" {
		seen[f.cursor] = true
	}
	for fetched := 0; fetched < f.maxPages; fetched++ {
		resp, err := cc.client.Do(cmd.Context(), http.MethodGet, crmDocumentQueryPath(op.pathFor(args), query), nil, crmDocumentHeaders(f.apiVersion, f.activeProfile))
		if err != nil {
			return reportCRMDocument(cmd, cc, err)
		}
		data, err := crmDocumentValidatedRead(op, args, query, resp)
		if err != nil {
			return reportCRMDocument(cmd, cc, err)
		}
		if op.action == "candidates" && data["available"] == false {
			// Preserve the producer's unavailable reason rather than pretending
			// it supplied a successful empty candidate collection.
			return output.PrintBody(cmd.OutOrStdout(), resp.Body, output.JSON)
		}
		for _, item := range data["items"].([]any) {
			if err := output.PrintJSON(cmd.OutOrStdout(), item); err != nil {
				return err
			}
		}
		if op.pagination == "page" {
			page, _ := crmDocumentInt(data["page"], false)
			perPage, _ := crmDocumentInt(data["per_page"], false)
			total, _ := crmDocumentInt(data["total"], true)
			if page*perPage >= total {
				return nil
			}
			if len(data["items"].([]any)) == 0 || page == 1000000 {
				return reportCRMDocument(cmd, cc, &apierr.TransportError{Err: errors.New("la página de documentos no avanza; salida parcial")})
			}
			query.Set("page", strconv.FormatInt(page+1, 10))
		} else {
			if data["next_cursor"] == nil {
				return nil
			}
			cursor := data["next_cursor"].(string)
			if seen[cursor] {
				return reportCRMDocument(cmd, cc, &apierr.TransportError{Err: errors.New("el cursor de documentos no avanza; salida parcial")})
			}
			seen[cursor] = true
			query.Set("cursor", cursor)
		}
	}
	return apierr.Usagef("se alcanzó --max-pages; salida parcial. Continúa con page=%s cursor=%q o aumenta el límite explícitamente", query.Get("page"), query.Get("cursor"))
}

func printCRMDocumentBody(w io.Writer, body []byte, data map[string]any, format output.Format, op crmDocumentOperation) error {
	if format == output.JSON {
		return output.PrintBody(w, body, format)
	}
	table := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	row := func(values ...any) error {
		cells := make([]string, len(values))
		for index, value := range values {
			if value == nil {
				cells[index] = "—"
			} else {
				cells[index] = strings.Map(func(r rune) rune {
					if r < 32 || r == 127 || (r >= 128 && r <= 159) {
						return ' '
					}
					return r
				}, fmt.Sprint(value))
			}
		}
		_, err := fmt.Fprintln(table, strings.Join(cells, "\t"))
		return err
	}
	if op.action == "financial-summary" {
		if err := row("DIVISA", "PREVISTO", "PRESUPUESTADO", "FACTURADO", "COBRADO"); err != nil {
			return err
		}
		for _, item := range data["currencies"].([]any) {
			o := item.(map[string]any)
			if err := row(o["currency"], o["forecast"], o["quoted"], o["invoiced"], o["paid"]); err != nil {
				return err
			}
		}
	} else if op.pagination != "" {
		if op.action == "candidates" && data["available"] == false {
			if err := row("DISPONIBLE", "MOTIVO"); err != nil {
				return err
			}
			if err := row(false, data["unavailable_reason"]); err != nil {
				return err
			}
		} else {
			if err := row("ID", "VERSION/REVISION", "TIPO", "TÍTULO/ESTADO", "DOCUMENTO", "PRIMARIO"); err != nil {
				return err
			}
			for _, item := range data["items"].([]any) {
				o := item.(map[string]any)
				version, label, kind := o["version"], o["title"], o["document_type"]
				if op.action == "candidates" {
					version, label, kind = o["revision"], o["label"], o["type"]
				}
				if op.group == "opportunity-documents" && op.action == "list" {
					label = o["status"]
				}
				if err := row(o["id"], version, kind, label, o["document_id"], o["is_primary"]); err != nil {
					return err
				}
			}
		}
		if op.pagination == "page" {
			if err := row("PÁGINA", data["page"], "POR PÁGINA", data["per_page"], "TOTAL", data["total"]); err != nil {
				return err
			}
		} else {
			if err := row("NEXT_CURSOR", data["next_cursor"]); err != nil {
				return err
			}
		}
	} else {
		if err := row("ID", "VERSION", "OPORTUNIDAD", "TIPO", "DOCUMENTO", "ESTADO"); err != nil {
			return err
		}
		if err := row(data["id"], data["version"], data["opportunity_id"], data["document_type"], data["document_id"], data["status"]); err != nil {
			return err
		}
		if op.group == "documents" && op.action == "get" {
			if title, exists := data["title"]; exists {
				if err := row("TÍTULO", title); err != nil {
					return err
				}
			}
		}
	}
	return table.Flush()
}

func crmDocumentManifest() []manifestEntry {
	entries := make([]manifestEntry, 0, len(crmDocumentOperations)+2)
	for _, op := range crmDocumentOperations {
		entry := manifestEntry{Command: "factuarea crm " + op.group + " " + op.action, OperationID: op.operationID, Summary: op.summary,
			Args: append([]string{}, op.args...), Flags: []flagInfo{}, Mutating: op.mutating(), Binary: op.action == "download", Paginated: op.pagination != "",
			Irreversible: op.confirmation(), RequiredScope: op.scope(), Example: crmDocumentExamples[op.action]}
		command := newCRMDocumentCommand(op)
		command.Flags().VisitAll(func(flag *pflag.Flag) { entry.Flags = append(entry.Flags, flagInfo{flag.Name, flag.Value.Type()}) })
		required, optional := crmDocumentBodyFields(op.action)
		for _, fields := range [][]string{required, optional} {
			for _, name := range fields {
				kind := "string"
				if name == "expected_version" {
					kind = "integer"
				}
				if name == "is_primary" {
					kind = "boolean"
				}
				if name == "draft" {
					kind = "object"
					entry.BodyHasObjects = true
				}
				req := false
				for _, field := range required {
					if field == name {
						req = true
					}
				}
				entry.BodyFields = append(entry.BodyFields, manifestField{Name: name, Type: kind, Kind: kind, Required: req})
			}
		}
		entries = append(entries, entry)
	}
	for _, group := range []string{"documents", "opportunity-documents"} {
		scope := "crm_documents:write"
		if group == "opportunity-documents" {
			scope = "crm_opportunitydocuments:write"
		}
		entries = append(entries, manifestEntry{Command: "factuarea crm " + group + " recover", Summary: "Replay explícito del intent original sellado; no endpoint adicional", Args: []string{},
			Flags: []flagInfo{{"intent-file", "string"}, {"confirm", "string"}, {"skip-scope-check", "boolean"}}, Mutating: true, RequiredScope: scope})
	}
	return entries
}
