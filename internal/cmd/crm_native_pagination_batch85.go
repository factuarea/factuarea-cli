package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/factuarea/factuarea-cli/internal/apierr"
	"github.com/factuarea/factuarea-cli/internal/output"
	"github.com/spf13/cobra"
)

type crmNativePagination struct {
	Path, Parameter, Items, Cursor, Page, PerPage, Total, LastPage string
	FixedPerPage                                                   int64
	HasMore                                                        string
}

// Exact published native paging contracts, including their distinct response envelopes.
// The three insights envelopes come from the actual CrmForecastWire owner.
// Contact duplicate/options pages use the owner repository PAGE_SIZE = 25.
// These constants contain no cursors, grants, subject data or caller-selected aliases.
var crmNativePaginationContracts = map[string]crmNativePagination{
	"crmConfigurationGetCrmOptions":                      {Path: "/crm/configuration/options", Parameter: "cursor", Items: "data", Cursor: "next_cursor"},
	"crmConfigurationListCrmConfiguration":               {Path: "/crm/configuration", Parameter: "cursor", Items: "data", Cursor: "next_cursor"},
	"crmConfigurationListCrmScoringPolicies":             {Path: "/crm/scoring-policies", Parameter: "cursor", Items: "data", Cursor: "next_cursor"},
	"crmContactPeopleDuplicates":                         {Path: "/crm/contact-people/{person}/duplicates", Parameter: "page", Items: "data", Page: "meta.current_page", Total: "meta.total", FixedPerPage: 25},
	"crmContactPeopleIndex":                              {Path: "/crm/contact-people", Parameter: "page", Items: "data", Page: "meta.current_page", Total: "meta.total", PerPage: "meta.per_page", LastPage: "meta.last_page"},
	"crmContactPeopleOptions":                            {Path: "/crm/contact-people/options", Parameter: "page", Items: "data", Page: "meta.current_page", Total: "meta.total", FixedPerPage: 25},
	"crmFindLeadDuplicates":                              {Path: "/crm/leads/{lead}/duplicates", Parameter: "cursor", Items: "data.data", Cursor: "data.next_cursor", HasMore: "data.has_more"},
	"crmFindLeadScoreRecalculationRun":                   {Path: "/crm/lead-score-runs/{run}", Parameter: "cursor", Items: "data.results", Cursor: "data.next_cursor", HasMore: "data.has_more"},
	"crmGetLeadHistory":                                  {Path: "/crm/leads/{lead}/history", Parameter: "cursor", Items: "data.data", Cursor: "data.next_cursor", HasMore: "data.has_more"},
	"crmGetSalesGoalDrillThrough":                        {Path: "/crm/sales-goals/{goal}/drill-through", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor"},
	"crmListCaptureForms":                                {Path: "/crm/capture/forms", Parameter: "cursor", Items: "data", Cursor: "next_cursor", HasMore: "has_more"},
	"crmListCaptureSubmissions":                          {Path: "/crm/capture/submissions", Parameter: "cursor", Items: "data", Cursor: "next_cursor", HasMore: "has_more"},
	"crmListCommercialDocuments":                         {Path: "/crm/documents", Parameter: "page", Items: "data.items", Page: "data.page", PerPage: "data.per_page", Total: "data.total"},
	"crmListCrmAccessPolicies":                           {Path: "/crm/access-policies", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor", HasMore: "data.has_more"},
	"crmListCrmActivities":                               {Path: "/crm/crmactivity", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor"},
	"crmListCrmAssignmentPolicies":                       {Path: "/crm/assignment-policies", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor", HasMore: "data.has_more"},
	"crmListCrmFieldAccessPolicies":                      {Path: "/crm/field-policies", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor", HasMore: "data.has_more"},
	"crmListLeads":                                       {Path: "/crm/leads", Parameter: "cursor", Items: "data.data", Cursor: "data.next_cursor", HasMore: "data.has_more"},
	"crmListLinkedCrmTasks":                              {Path: "/crm/task-links", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor", HasMore: "data.has_more"},
	"crmListOpportunities":                               {Path: "/crm/opportunity", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor"},
	"crmListOpportunityDocumentCandidates":               {Path: "/crm/opportunities/{opportunity}/documents/candidates", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor"},
	"crmListOpportunityDocuments":                        {Path: "/crm/opportunitydocuments/{opportunity}/documents", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor"},
	"crmListSalesGoals":                                  {Path: "/crm/sales-goals", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor"},
	"crmListSalesTeams":                                  {Path: "/crm/sales-teams", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor", HasMore: "data.has_more"},
	"crmSearchKnowledgeArticles":                         {Path: "/crm/knowledge/articles", Parameter: "page", Items: "data.items", Page: "data.page", PerPage: "data.per_page", Total: "data.total"},
	"crmSearchLeads":                                     {Path: "/crm/leads/search", Parameter: "cursor", Items: "data.data", Cursor: "data.next_cursor", HasMore: "data.has_more"},
	"getCrmServiceInsightDrillThrough":                   {Path: "/crm/service-insights/drill", Parameter: "page", Items: "data.items", Page: "data.page", PerPage: "data.per_page", Total: "data.total"},
	"getServiceBillableTime":                             {Path: "/crm/service-tickets/{ticket}/billable-time", Parameter: "cursor", Items: "data.entries", Cursor: "data.next_cursor"},
	"getServiceSlaHistory":                               {Path: "/crm/service-tickets/{ticket}/sla/history", Parameter: "page", Items: "data.items", Page: "data.page", PerPage: "data.per_page", Total: "data.total"},
	"getServiceTicketTimeline":                           {Path: "/crm/service-tickets/{ticket}/timeline", Parameter: "page", Items: "data.items", Page: "data.page", PerPage: "data.per_page", Total: "data.total"},
	"getServiceTimesheet":                                {Path: "/crm/service-tickets/{ticket}/timesheet", Parameter: "cursor", Items: "data.entries", Cursor: "data.next_cursor"},
	"listBookingServices":                                {Path: "/crm/booking/services", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor"},
	"listBookings":                                       {Path: "/crm/booking/bookings", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor"},
	"listCrmWhatsAppMessageTemplates":                    {Path: "/crm/conversation/{conversation}/messages/{message}/whatsapp-cloud/templates", Parameter: "cursor", Items: "data.templates", Cursor: "data.next_cursor"},
	"listPipelines":                                      {Path: "/crm/pipelines", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor", HasMore: "data.has_more"},
	"listServiceCalendars":                               {Path: "/crm/service-calendars", Parameter: "page", Items: "data.items", Page: "data.page", PerPage: "data.per_page", Total: "data.total"},
	"listServiceTickets":                                 {Path: "/crm/service-tickets", Parameter: "page", Items: "data.items", Page: "data.page", PerPage: "data.per_page", Total: "data.total"},
	"public-api.v1.crm-bottle-migrations.source-preview": {Path: "/crm/bottle-migrations/source-preview", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor"},
	"public-api.v1.crm-insights.list-definitions":        {Path: "/crm/report-definitions", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor"},
	"public-api.v1.crm-insights.run-definition":          {Path: "/crm/report-definitions/{definition}/result", Parameter: "cursor", Items: "data.rows", Cursor: "data.next_cursor"},
	"public-api.v1.crm-insights.stalled":                 {Path: "/crm/crminsights/stalled", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor"},
	"v1.crm.sequences.list":                              {Path: "/crm/crmsequence", Parameter: "cursor", Items: "data.items", Cursor: "data.next_cursor"},
}

func crmNativePaginationFor(op genOp) (crmNativePagination, bool) {
	plan, known := crmNativePaginationContracts[op.OperationID]
	if !known {
		return crmNativePagination{}, false
	}
	if op.Method != http.MethodGet || strings.TrimPrefix(op.Path, "/v1") != plan.Path || op.Body != nil {
		panic("native CRM pagination route drift")
	}
	found := false
	for _, parameter := range op.QueryParams {
		if parameter.Name == plan.Parameter {
			found = true
		}
	}
	if !found {
		panic("native CRM pagination parameter drift")
	}
	return plan, true
}

func crmNativePageNumber(value string) (int64, error) {
	if value == "" {
		return 1, nil
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < 1 || strconv.FormatInt(number, 10) != value {
		return 0, apierr.Usagef("page debe conservar un entero positivo nativo")
	}
	return number, nil
}

func crmNativeRawAt(body json.RawMessage, path string) (json.RawMessage, error) {
	for _, key := range strings.Split(path, ".") {
		var object map[string]json.RawMessage
		if json.Unmarshal(body, &object) != nil || object == nil {
			return nil, crmOAuthContractError()
		}
		var present bool
		body, present = object[key]
		if !present {
			return nil, crmOAuthContractError()
		}
	}
	return body, nil
}

func crmNativeIntegerAt(body json.RawMessage, path string, minimum int64) (int64, error) {
	raw, err := crmNativeRawAt(body, path)
	if err != nil {
		return 0, err
	}
	var number int64
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &number) != nil || number < minimum {
		return 0, crmOAuthContractError()
	}
	return number, nil
}

func crmNativePageState(plan crmNativePagination, body []byte, current string) (string, bool, error) {
	if !utf8.Valid(body) || !json.Valid(body) {
		return "", false, crmOAuthContractError()
	}
	raw, err := crmNativeRawAt(body, plan.Items)
	if err != nil {
		return "", false, err
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil {
		return "", false, crmOAuthContractError()
	}
	if plan.Parameter == "cursor" {
		raw, err = crmNativeRawAt(body, plan.Cursor)
		if err != nil {
			return "", false, err
		}
		var next *string
		if json.Unmarshal(raw, &next) != nil || (next != nil && *next == "") {
			return "", false, crmOAuthContractError()
		}
		if plan.HasMore != "" {
			raw, err = crmNativeRawAt(body, plan.HasMore)
			var more bool
			if err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &more) != nil || more != (next != nil) {
				return "", false, crmOAuthContractError()
			}
		}
		if next == nil {
			return "", false, nil
		}
		return *next, true, nil
	}
	requested, err := crmNativePageNumber(current)
	if err != nil {
		return "", false, err
	}
	page, err := crmNativeIntegerAt(body, plan.Page, 1)
	if err != nil || page != requested {
		return "", false, crmOAuthContractError()
	}
	total, err := crmNativeIntegerAt(body, plan.Total, 0)
	if err != nil {
		return "", false, err
	}
	size := plan.FixedPerPage
	if size == 0 {
		size, err = crmNativeIntegerAt(body, plan.PerPage, 1)
		if err != nil {
			return "", false, err
		}
	}
	if int64(len(items)) > size || int64(len(items)) > total {
		return "", false, crmOAuthContractError()
	}
	last := total / size
	if total%size != 0 {
		last++
	}
	if last == 0 {
		last = 1
	}
	if plan.LastPage != "" {
		actual, err := crmNativeIntegerAt(body, plan.LastPage, 1)
		if err != nil || actual != last {
			return "", false, crmOAuthContractError()
		}
	}
	if page >= last {
		return "", false, nil
	}
	if len(items) == 0 || page == int64(^uint64(0)>>1) {
		return "", false, crmOAuthContractError()
	}
	return strconv.FormatInt(page+1, 10), true, nil
}

func runCRMNativePages(command *cobra.Command, cc *cliContext, op genOp, plan crmNativePagination, path string, query url.Values) error {
	budget, _ := command.Flags().GetInt("max-pages")
	if budget < 1 || budget > 1000 {
		return apierr.Usagef("max-pages debe estar entre 1 y 1000")
	}
	if plan.Parameter == "page" {
		if _, err := crmNativePageNumber(query.Get("page")); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	if first := query.Get(plan.Parameter); first != "" {
		seen[first] = true
	}
	for page := 0; page < budget; page++ {
		endpoint := path
		if len(query) > 0 {
			endpoint += "?" + query.Encode()
		}
		response, err := cc.client.Do(command.Context(), http.MethodGet, endpoint, nil, nil)
		if err != nil {
			return err
		}
		next, more, err := crmNativePageState(plan, response.Body, query.Get(plan.Parameter))
		if err != nil {
			return err
		}
		if more && seen[next] {
			return crmOAuthContractError()
		}
		// Keep each complete native envelope, including secondary arrays, C7, nulls
		// and factual totals. Compact changes only JSON whitespace, never numbers.
		var wire bytes.Buffer
		if err = json.Compact(&wire, response.Body); err != nil {
			return crmOAuthContractError()
		}
		if err = output.PrintBody(command.OutOrStdout(), wire.Bytes(), cc.format); err != nil {
			return err
		}
		if !more {
			return nil
		}
		seen[next] = true
		query.Set(plan.Parameter, next)
	}
	details := map[string]any{"partial": true}
	details["next_"+plan.Parameter] = query.Get(plan.Parameter)
	return &apierr.APIError{Type: "invalid_request_error", Code: "pagination_budget_exhausted", Message: "presupuesto max-pages agotado; la salida contiene sólo las páginas ya consultadas", Details: details}
}
