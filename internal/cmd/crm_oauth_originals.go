package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type crmOAuthOriginalOperation struct {
	name, operation, path, scope, oauthScope string
	stage                                    bool
	nativeID                                 string
}

var crmOAuthOriginalOperations = []crmOAuthOriginalOperation{
	{"lead-create", "crm_leads.create", "/v1/crm/leads/create/original", "crm_leads:write", "crm_leads.write", false, "crmGetLeadCreateOriginal"},
	{"opportunity-create", "crm_opportunities.create", "/v1/crm/opportunity/create/original", "crm_opportunities:write", "crm_opportunities.write", false, "crmGetOpportunityCreateOriginal"},
	{"opportunity-stage", "crm_opportunities.move_stage", "/v1/crm/opportunity/{opportunity}/stage/original", "crm_opportunities:write", "crm_opportunities.write", true, "crmGetOpportunityStageOriginal"},
	{"activity-create", "crm_activities.create", "/v1/crm/crmactivity/create/original", "crm_activities:write", "crm_activities.write", false, "crmGetCrmActivityCreateOriginal"},
}
var crmOAuthUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var crmOAuthFineScope = regexp.MustCompile(`^[a-z][a-z0-9_]*:[a-z][a-z0-9_]*$`)

type crmOAuthCredentialContext struct {
	CompanyID            string   `json:"company_id"`
	CredentialID         string   `json:"credential_id"`
	ClientID             string   `json:"client_id"`
	Environment          string   `json:"environment"`
	Scopes               []string `json:"scopes"`
	AuthorizationVersion string   `json:"authorization_version"`
}
type crmOAuthOriginalReceipt struct {
	State      string `json:"state"`
	BodySHA256 string `json:"original_body_sha256"`
	Original   *struct {
		Status int     `json:"status"`
		Body   *string `json:"body"`
	} `json:"original_response"`
}

func registerCRMOAuthOriginalCommands(root *cobra.Command) {
	registerCRMOAuthOriginalCommandsFor(root, generatedOps())
}

func registerCRMOAuthOriginalCommandsFor(root *cobra.Command, generated []genOp) {
	contextMissing, missing := crmOAuthMissingGeneratedCommands(generated)
	if !contextMissing && len(missing) == 0 {
		return
	}
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
	if contextMissing {
		crm.AddCommand(newCRMOAuthCredentialContextCommand())
	}
	if len(missing) == 0 {
		return
	}
	var originals *cobra.Command
	for _, child := range crm.Commands() {
		if child.Name() == "originals" {
			originals = child
			break
		}
	}
	if originals == nil {
		originals = serviceLevelGroup("originals", "Recupera el recibo original con GET; nunca repite una mutación")
		crm.AddCommand(originals)
	}
	for _, op := range missing {
		originals.AddCommand(newCRMOAuthOriginalCommand(op))
	}
}

// Bind only the owner's exact published IDs to the existing native readers.
// A changed verb, URL, writer scope or retained-intent shape is a contract drift,
// never a reason to fall back to the generic HTTP/body builder.
func crmOAuthGeneratedCommand(op genOp) (*cobra.Command, bool) {
	if op.OperationID == "crmGetOAuthCredentialContext" {
		if op.Method != http.MethodGet || strings.TrimPrefix(op.Path, "/v1") != "/crm/credential-context" || op.RequiredScope != "crm_access_context:read" || op.Body != nil || len(op.QueryParams) != 0 || len(op.PathParams) != 0 || op.IdempotencyRequired || op.Irreversible || len(op.Groups) != 1 || op.Groups[0] != "crm" || op.Action != "credential-context" {
			panic("native CRM credential-context contract drift")
		}
		return newCRMOAuthCredentialContextCommand(), true
	}
	for _, native := range crmOAuthOriginalOperations {
		if op.OperationID != native.nativeID {
			continue
		}
		if op.Method != http.MethodGet || strings.TrimPrefix(op.Path, "/v1") != strings.TrimPrefix(native.path, "/v1") || op.RequiredScope != native.scope || op.Body != nil || !op.IdempotencyRequired || op.Irreversible || len(op.QueryParams) != 1 || op.QueryParams[0].Name != "original_body_sha256" || !op.QueryParams[0].Required || op.QueryParams[0].Type != "string" || len(op.Groups) != 2 || op.Groups[0] != "crm" || op.Groups[1] != "originals" || op.Action != native.name {
			panic("native CRM original receipt contract drift")
		}
		if (!native.stage && len(op.PathParams) != 0) || (native.stage && (len(op.PathParams) != 1 || op.PathParams[0].Name != "opportunity" || !op.PathParams[0].Required || op.PathParams[0].Type != "string")) {
			panic("native CRM original receipt target drift")
		}
		return newCRMOAuthOriginalCommand(native), true
	}
	return nil, false
}

func crmOAuthMissingGeneratedCommands(generated []genOp) (bool, []crmOAuthOriginalOperation) {
	contextMissing := true
	seen := map[string]bool{}
	for _, op := range generated {
		if _, native := crmOAuthGeneratedCommand(op); !native {
			continue
		}
		if seen[op.OperationID] {
			panic("duplicate native CRM original operation")
		}
		seen[op.OperationID] = true
		if op.OperationID == "crmGetOAuthCredentialContext" {
			contextMissing = false
		}
	}
	var missing []crmOAuthOriginalOperation
	for _, op := range crmOAuthOriginalOperations {
		if !seen[op.nativeID] {
			missing = append(missing, op)
		}
	}
	return contextMissing, missing
}

func newCRMOAuthCredentialContextCommand() *cobra.Command {
	var tokenEnv, version string
	c := &cobra.Command{Use: "credential-context", Short: "Contexto OAuth actual (crm_access_context:read; OAuth crm_access_context.read)", Args: UsageArgs(cobra.NoArgs)}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		if tokenEnv == "" {
			return apierr.Usagef("credential-context requiere --oauth-token-env con un token OAuth nativo")
		}
		wire, err := crmOAuthOriginalGET(cmd, "/v1/crm/credential-context", nil, "", tokenEnv, version)
		if err != nil {
			return err
		}
		var payload struct {
			Data crmOAuthCredentialContext `json:"data"`
		}
		if err := json.Unmarshal(wire, &payload); err != nil {
			return crmOAuthContractError()
		}
		data := payload.Data
		if !crmOAuthUUID.MatchString(data.CompanyID) || !crmOAuthUUID.MatchString(data.CredentialID) || !crmOAuthUUID.MatchString(data.ClientID) || (data.Environment != "test" && data.Environment != "live") || data.AuthorizationVersion == "" || data.Scopes == nil {
			return crmOAuthContractError()
		}
		seen := map[string]bool{}
		for _, scope := range data.Scopes {
			if (scope != "*" && !crmOAuthFineScope.MatchString(scope)) || seen[scope] {
				return crmOAuthContractError()
			}
			seen[scope] = true
		}
		if globalsFrom(cmd).JSON {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(wire))
			return err
		}
		table := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		if _, err = fmt.Fprintf(table, "EMPRESA\tCREDENCIAL\tCLIENTE\tENTORNO\tAUTORIZACIÓN\n%s\t%s\t%s\t%s\t%s\n", data.CompanyID, data.CredentialID, data.ClientID, data.Environment, data.AuthorizationVersion); err != nil {
			return err
		}
		return table.Flush()
	}
	crmOAuthFlags(c, &tokenEnv, &version)
	return c
}

func newCRMOAuthOriginalCommand(op crmOAuthOriginalOperation) *cobra.Command {
	var tokenEnv, version, bodyFile, key, bodySHA string
	use := op.name
	args := cobra.NoArgs
	if op.stage {
		use += " opportunity"
		args = cobra.ExactArgs(1)
	}
	c := &cobra.Command{Use: use, Short: "GET original (" + op.scope + "; OAuth " + op.oauthScope + ")", Args: UsageArgs(args)}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if (bodyFile == "") == (bodySHA == "") || !crmOAuthRawKey(key) {
			return apierr.Usagef("requiere first-body-file o original-body-sha256 (una sola fuente) e idempotency-key original ASCII")
		}
		digest := bodySHA
		if bodyFile != "" {
			body, err := os.ReadFile(bodyFile)
			if err != nil {
				return apierr.ReadFileUsagef(bodyFile, err)
			}
			var object map[string]json.RawMessage
			if !utf8.Valid(body) || json.Unmarshal(body, &object) != nil || object == nil {
				return apierr.Usagef("first-body-file debe conservar los bytes del primer objeto JSON UTF-8 enviado")
			}
			sum := sha256.Sum256(body)
			digest = hex.EncodeToString(sum[:])
		} else if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(digest) {
			return apierr.Usagef("original-body-sha256 debe ser el hash lowercase de los bytes del primer envío")
		}
		path := op.path
		if op.stage {
			if !crmOAuthUUID.MatchString(args[0]) {
				return apierr.Usagef("opportunity debe ser un UUID nativo")
			}
			path = strings.ReplaceAll(path, "{opportunity}", strings.ToLower(args[0]))
		}
		wire, err := crmOAuthOriginalGET(cmd, path, url.Values{"original_body_sha256": {digest}}, key, tokenEnv, version)
		if err != nil {
			return err
		}
		var payload struct {
			Data crmOAuthOriginalReceipt `json:"data"`
			Meta struct {
				Version string `json:"authorization_version"`
			} `json:"meta"`
		}
		if json.Unmarshal(wire, &payload) != nil || payload.Data.BodySHA256 != digest || payload.Meta.Version == "" {
			return crmOAuthContractError()
		}
		var fields struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		if json.Unmarshal(wire, &fields) != nil || fields.Data["original_response"] == nil {
			return crmOAuthContractError()
		}
		data := payload.Data
		switch data.State {
		case "confirmed":
			if data.Original == nil || data.Original.Status < 200 || data.Original.Status > 299 || data.Original.Body == nil || !json.Valid([]byte(*data.Original.Body)) {
				return crmOAuthContractError()
			}
		case "pending", "in_progress", "unconfirmed", "missing":
			if data.Original != nil {
				return crmOAuthContractError()
			}
		default:
			return crmOAuthContractError()
		}
		if globalsFrom(cmd).JSON {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(wire))
			return err
		}
		table := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		if _, err = fmt.Fprintf(table, "ESTADO\tSHA256 ORIGINAL\tSTATUS ORIGINAL\n%s\t%s\t", data.State, digest); err != nil {
			return err
		}
		if data.Original != nil {
			if _, err = fmt.Fprintf(table, "%d\n", data.Original.Status); err != nil {
				return err
			}
		} else {
			if _, err = fmt.Fprintln(table, ""); err != nil {
				return err
			}
		}
		return table.Flush()
	}
	crmOAuthFlags(c, &tokenEnv, &version)
	c.Flags().StringVar(&bodyFile, "first-body-file", "", "bytes exactos del primer cuerpo enviado; nunca se reconstruye JSON")
	c.Flags().StringVar(&bodySHA, "original-body-sha256", "", "hash exacto emitido por write_unconfirmed; alternativa a first-body-file")
	c.Flags().StringVar(&key, "idempotency-key", "", "clave original exacta; no se genera ni se recorta")
	return c
}
func crmOAuthFlags(c *cobra.Command, tokenEnv, version *string) {
	c.Flags().StringVar(tokenEnv, "oauth-token-env", "", "nombre de variable con el access token OAuth actual; vacío conserva la API key real")
	c.Flags().StringVar(version, "api-version", "", "Factuarea-Version (YYYY-MM-DD)")
}
func crmOAuthRawKey(key string) bool {
	if len(key) < 1 || len(key) > 255 || strings.TrimSpace(key) != key {
		return false
	}
	for _, b := range []byte(key) {
		if b < 0x20 || b > 0x7e {
			return false
		}
	}
	return true
}
func crmOAuthContractError() error {
	return &apierr.TransportError{Err: fmt.Errorf("respuesta nativa incompatible con el contexto o recibo original")}
}

// Actual OAuth and actual API keys remain distinct credential sources. No account preflight or fabricated family.
func crmOAuthOriginalGET(cmd *cobra.Command, path string, query url.Values, key, tokenEnv, version string) ([]byte, error) {
	if err := validateCRMDocumentHeaders(version, ""); err != nil {
		return nil, err
	}
	g := globalsFrom(cmd)
	var token string
	if tokenEnv != "" {
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(tokenEnv) {
			return nil, apierr.Usagef("oauth-token-env debe nombrar una variable de entorno")
		}
		token = os.Getenv(tokenEnv)
		if token == "" || strings.HasPrefix(token, "fact_test_") || strings.HasPrefix(token, "fact_live_") {
			return nil, apierr.Usagef("la variable debe contener un access token OAuth nativo")
		}
		for _, b := range []byte(token) {
			if b < 0x21 || b > 0x7e {
				return nil, apierr.Usagef("access token OAuth inválido")
			}
		}
		if g.Profile != "" {
			return nil, apierr.Usagef("--profile selecciona API keys; OAuth usa el contexto actual del emisor")
		}
	} else {
		cc, err := newCLIContext(g, "")
		if err != nil {
			return nil, err
		}
		token = cc.res.APIKey
	}
	base := os.Getenv(envBaseURL)
	if base == "" {
		base = "https://api.factuarea.com"
	}
	if err := validateBaseURLTransport(base, g.AllowInsecureTransport); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, apierr.Usagef("FACTUAREA_BASE_URL debe conservar un servidor HTTP sin credenciales, query ni fragmento")
	}
	endpoint := strings.TrimRight(base, "/") + path
	if len(query) != 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, crmOAuthContractError()
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if version != "" {
		req.Header.Set("Factuarea-Version", version)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	defer transport.CloseIdleConnections()
	hc := &http.Client{Timeout: 60 * time.Second, Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := hc.Do(req)
	if err != nil {
		return nil, &apierr.TransportError{Err: fmt.Errorf("no se pudo consultar el GET original; no se ha repetido ninguna mutación")}
	}
	defer response.Body.Close()
	wire, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, crmOAuthContractError()
	}
	if response.StatusCode != http.StatusOK {
		var payload struct {
			Error struct {
				Type    string `json:"type"`
				Code    string `json:"code"`
				Message string `json:"message"`
				Subcode string `json:"subcode"`
			} `json:"error"`
			RequestID string `json:"request_id"`
		}
		_ = json.Unmarshal(wire, &payload)
		if payload.Error.Message == "" {
			payload.Error.Message = "El GET nativo original fue rechazado"
		}
		if payload.RequestID == "" {
			payload.RequestID = response.Header.Get("X-Request-Id")
		}
		redact := func(value string) string { return strings.ReplaceAll(value, token, "[REDACTED]") }
		return nil, &apierr.APIError{StatusCode: response.StatusCode, Type: redact(payload.Error.Type), Code: redact(payload.Error.Code), Message: redact(payload.Error.Message), Subcode: redact(payload.Error.Subcode), RequestID: redact(payload.RequestID), RetryAfter: response.Header.Get("Retry-After")}
	}
	if !strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "application/json") || response.Header.Get("Location") != "" {
		return nil, crmOAuthContractError()
	}
	return wire, nil
}

func crmOAuthOriginalManifest() []manifestEntry {
	return crmOAuthOriginalManifestFor(generatedOps())
}

func crmOAuthOriginalManifestFor(generated []genOp) []manifestEntry {
	contextMissing, missing := crmOAuthMissingGeneratedCommands(generated)
	result := []manifestEntry{}
	if contextMissing {
		contextCmd := newCRMOAuthCredentialContextCommand()
		contextEntry := manifestEntry{Command: "factuarea crm credential-context", OperationID: "crm_access_context.show", RequiredScope: "crm_access_context:read", Summary: contextCmd.Short, Args: []string{}, Flags: []flagInfo{}}
		contextCmd.Flags().VisitAll(func(flag *pflag.Flag) {
			contextEntry.Flags = append(contextEntry.Flags, flagInfo{flag.Name, flag.Value.Type()})
		})
		result = append(result, contextEntry)
	}
	for _, op := range missing {
		command := newCRMOAuthOriginalCommand(op)
		entry := manifestEntry{Command: "factuarea crm originals " + op.name, OperationID: op.operation, RequiredScope: op.scope, Summary: command.Short, Args: []string{}, Flags: []flagInfo{}}
		if op.stage {
			entry.Args = []string{"opportunity"}
		}
		command.Flags().VisitAll(func(flag *pflag.Flag) { entry.Flags = append(entry.Flags, flagInfo{flag.Name, flag.Value.Type()}) })
		result = append(result, entry)
	}
	return result
}
