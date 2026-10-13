package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/spf13/cobra"
)

// These descriptors bind current owner routes while the final OpenAPI is being
// regenerated. The existing Cobra/body/output/client owners execute every call.
// Generated method+path entries take precedence, so regeneration adds no alias.
func crmCoreNativeOps() []genOp {
	var ops []genOp
	add := func(group, operation, method, path, action, scope string, query []genParam) {
		op := genOp{OperationID: "public-api.v1." + group + "." + operation, Groups: []string{group}, Action: action, Method: method, Path: path,
			RequiredScope: scope, Summary: "CRM nativo: " + action + " (" + scope + ")", QueryParams: query}
		for _, match := range regexp.MustCompile(`\{([^}]+)\}`).FindAllStringSubmatch(path, -1) {
			op.PathParams = append(op.PathParams, genParam{Name: match[1], In: "path", Type: "string", Required: true})
		}
		if method != "GET" {
			op.Body = &genBody{Kind: "json"}
		}
		ops = append(ops, op)
	}
	page := []genParam{{Name: "filters", In: "query", Type: "string", Description: "objeto JSON de filtros públicos del dueño"}, {Name: "cursor", In: "query", Type: "string", Description: "cursor opaco de la página anterior"}, {Name: "limit", In: "query", Type: "integer", Description: "tamaño de página autorizado"}}
	leadsPage := append(append([]genParam{}, page...), genParam{Name: "search", In: "query", Type: "string"}, genParam{Name: "sort", In: "query", Type: "string"})
	add("crm-leads", "index", "GET", "/crm/leads", "index", "crm_leads:read", leadsPage)
	add("crm-opportunities", "list", "GET", "/crm/opportunity", "list", "crm_opportunities:read", page)
	add("crm-activities", "index", "GET", "/crm/crmactivity", "index", "crm_activities:read", page)
	for _, owner := range []struct{ group, scope, path, param string }{{"crm-leads", "crm_leads", "/crm/leads", "lead"}, {"crm-opportunities", "crm_opportunities", "/crm/opportunity", "opportunity"}, {"crm-activities", "crm_activities", "/crm/crmactivity", "activity"}} {
		add(owner.group, "show", "GET", owner.path+"/{"+owner.param+"}", "show", owner.scope+":read", nil)
		add(owner.group, "create", "POST", owner.path, "create", owner.scope+":write", nil)
		add(owner.group, "update", "PATCH", owner.path+"/{"+owner.param+"}", "update", owner.scope+":write", nil)
	}
	add("crm-opportunities", "move-stage", "POST", "/crm/opportunity/{opportunity}/stage", "move-stage", "crm_opportunities:write", nil)
	add("crm-opportunities", "close", "POST", "/crm/opportunity/{opportunity}/close", "close", "crm_opportunities:write", nil)
	add("crm-opportunities", "reopen", "POST", "/crm/opportunity/{opportunity}/reopen", "reopen", "crm_opportunities:write", nil)
	add("crm-activities", "transition", "POST", "/crm/crmactivity/{activity}/status", "transition", "crm_activities:transition", nil)
	return ops
}

func crmCoreMissingOps(generated []genOp) []genOp {
	var missing []genOp
	for _, op := range crmCoreNativeOps() {
		found := false
		for _, existing := range generated {
			if existing.Method == op.Method && strings.TrimPrefix(existing.Path, "/v1") == op.Path {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, op)
		}
	}
	return missing
}

func registerCRMCoreNativeCommands(root *cobra.Command) {
	for _, op := range crmCoreMissingOps(generatedOps()) {
		var group *cobra.Command
		for _, child := range root.Commands() {
			if child.Name() == op.Groups[0] {
				group = child
				break
			}
		}
		if group == nil {
			group = serviceLevelGroup(op.Groups[0], "CRM público nativo")
			root.AddCommand(group)
		}
		for _, child := range group.Commands() {
			if child.Name() == op.Action {
				panic("duplicate CRM core command")
			}
		}
		command := buildGeneratedCommand(op)
		group.AddCommand(command)
	}
}

func crmCoreNativeManifest() []manifestEntry {
	var entries []manifestEntry
	for _, op := range crmCoreMissingOps(generatedOps()) {
		entry := manifestEntry{Command: commandPath(op), OperationID: op.OperationID, Summary: op.Summary, RequiredScope: op.RequiredScope, Mutating: op.isMutating(), Args: []string{}, Flags: []flagInfo{}, Paginated: op.Method == "GET" && len(op.PathParams) == 0}
		for _, param := range op.PathParams {
			entry.Args = append(entry.Args, param.Name)
		}
		for _, param := range op.QueryParams {
			entry.Flags = append(entry.Flags, flagInfo{Name: param.Name, Type: param.Type})
		}
		if op.Body != nil {
			entry.Flags = append(entry.Flags, flagInfo{Name: "data-file", Type: "string"}, flagInfo{Name: "dry-run", Type: "bool"}, flagInfo{Name: "idempotency-key", Type: "string"})
		}
		if entry.Paginated {
			entry.Flags = append(entry.Flags, flagInfo{Name: "all", Type: "bool"}, flagInfo{Name: "max-pages", Type: "int"})
		}
		entries = append(entries, entry)
	}
	return entries
}

func crmOriginalForWrite(method, path string) string {
	if method != "POST" {
		return ""
	}
	path = strings.TrimPrefix(path, "/v1")
	switch path {
	case "/crm/leads":
		return "lead-create"
	case "/crm/opportunity":
		return "opportunity-create"
	case "/crm/crmactivity":
		return "activity-create"
	}
	if regexp.MustCompile(`^/crm/opportunity/[^/]+/stage$`).MatchString(path) {
		return "opportunity-stage"
	}
	return ""
}

func isCRMCoreOperation(op genOp) bool {
	for _, native := range crmCoreNativeOps() {
		if native.Method == op.Method && native.Path == strings.TrimPrefix(op.Path, "/v1") {
			return true
		}
	}
	return false
}

func saveCRMFirstWire(path string, body []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return apierr.WriteFileUsagef(path, err)
	}
	_, err = file.Write(body)
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(path)
		return apierr.WriteFileUsagef(path, err)
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return apierr.WriteFileUsagef(path, closeErr)
	}
	return nil
}

// The three owners expose different native page envelopes. Preserve each row
// and opaque cursor; never treat a page cursor as an entity id or GET URL.
func crmCorePage(body []byte, lead bool) ([]json.RawMessage, *string, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return nil, nil, crmOAuthContractError()
	}
	var page struct {
		Items  []json.RawMessage `json:"items"`
		Data   []json.RawMessage `json:"data"`
		Cursor *string           `json:"next_cursor"`
	}
	if json.Unmarshal(envelope.Data, &page) != nil {
		return nil, nil, crmOAuthContractError()
	}
	items := page.Items
	if lead {
		items = page.Data
	}
	if items == nil {
		return nil, nil, crmOAuthContractError()
	}
	if page.Cursor != nil && (*page.Cursor == "" || len(*page.Cursor) > 8192) {
		return nil, nil, crmOAuthContractError()
	}
	return items, page.Cursor, nil
}

func runCRMCorePages(command *cobra.Command, cc *cliContext, op genOp, path string, query url.Values) error {
	budget, _ := command.Flags().GetInt("max-pages")
	if budget < 1 || budget > 1000 {
		return apierr.Usagef("max-pages debe estar entre 1 y 1000")
	}
	seen := map[string]bool{}
	if first := query.Get("cursor"); first != "" {
		seen[first] = true
	}
	for page := 0; page < budget; page++ {
		endpoint := path
		if len(query) > 0 {
			endpoint += "?" + query.Encode()
		}
		response, err := cc.client.Do(command.Context(), "GET", endpoint, nil, nil)
		if err != nil {
			return err
		}
		items, cursor, err := crmCorePage(response.Body, op.Path == "/crm/leads")
		if err != nil {
			return err
		}
		for _, item := range items {
			if err = output.PrintBody(command.OutOrStdout(), item, cc.format); err != nil {
				return err
			}
		}
		if cursor == nil {
			return nil
		}
		if seen[*cursor] {
			return crmOAuthContractError()
		}
		seen[*cursor] = true
		query.Set("cursor", *cursor)
	}
	return &apierr.APIError{Type: "invalid_request_error", Code: "pagination_budget_exhausted", Message: "presupuesto max-pages agotado; la salida contiene sólo las páginas ya consultadas", Details: map[string]any{"partial": true, "next_cursor": query.Get("cursor")}}
}

func printCRMCoreBody(command *cobra.Command, op genOp, body []byte, format output.Format) error {
	if format == output.JSON {
		return output.PrintBody(command.OutOrStdout(), body, format)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return crmOAuthContractError()
	}
	rows := []json.RawMessage{envelope.Data}
	if op.Method == "GET" && len(op.PathParams) == 0 {
		var err error
		rows, _, err = crmCorePage(body, op.Path == "/crm/leads")
		if err != nil {
			return err
		}
	}
	table := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "ID\tVERSIÓN\tESTADO\tNOMBRE/TÍTULO")
	for _, wire := range rows {
		var row map[string]json.RawMessage
		if json.Unmarshal(wire, &row) != nil {
			return crmOAuthContractError()
		}
		value := func(key string) string {
			var text string
			if json.Unmarshal(row[key], &text) == nil {
				return strings.Map(func(r rune) rune {
					if r < 32 || r == 127 {
						return ' '
					}
					return r
				}, text)
			}
			return string(row[key])
		}
		label := value("title")
		if label == "" {
			label = value("name")
		}
		id, state := value("id"), value("status")
		if id == "" {
			id = value("activity_id")
		}
		if state == "" {
			state = value("state")
		}
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", id, value("version"), state, label)
	}
	return table.Flush()
}
