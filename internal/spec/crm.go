package spec

import (
	"fmt"
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// Native CRM operation identifiers are preserved exactly. These names describe
// CLI groups; they do not create or rename public operations.
func resolveCRM(op *v3.Operation, method, path string) (groups []string, action string, ok bool) {
	native := stringExt(op.Extensions, "x-crm-operation")
	family, suffix, found := strings.Cut(native, ".")
	if !found || suffix == "" {
		return nil, "", false
	}
	group := map[string]string{"crm_contact_people": "contact-people", "crm_leads": "leads", "crm_pipelines": "pipelines"}[family]
	if group == "" {
		return nil, "", false
	}
	scope := stringExt(op.Extensions, "x-required-scope")
	if scope != family+":read" && scope != family+":write" && scope != family+":delete" && scope != family+":export" {
		return nil, "", false
	}
	nativePath := strings.TrimPrefix(path, "/v1")
	expectedPath := "/crm/" + group
	if family == "crm_leads" && strings.Contains(suffix, "score_recalculation") {
		expectedPath = "/crm/lead-score-runs"
	}
	if family == "crm_pipelines" && strings.HasSuffix(suffix, "playbook") {
		expectedPath = "/crm/stages/"
	}
	if nativePath != expectedPath && !strings.HasPrefix(nativePath, strings.TrimRight(expectedPath, "/")+"/") {
		return nil, "", false
	}
	groups = []string{"crm", group}
	action = ToKebab(suffix)
	if suffix == "index" {
		action = "list"
	}
	if family == "crm_leads" && strings.Contains(suffix, "score_recalculation") {
		groups = []string{"crm", "lead-score-runs"}
		action = map[string]string{"preview_score_recalculation": "preview", "apply_score_recalculation": "apply", "find_score_recalculation_run": "show"}[suffix]
		if action == "" {
			return nil, "", false
		}
	}
	if family == "crm_pipelines" && strings.HasSuffix(suffix, "playbook") {
		groups = []string{"crm", "stages", "playbook"}
		action = map[string]string{"create_playbook": "create", "save_playbook": "save", "publish_playbook": "publish", "find_playbook": "show"}[suffix]
		if action == "" {
			return nil, "", false
		}
	}
	return groups, action, true
}

// Pagination is inferred from the owner's declared successful response, never
// from an assumed common envelope. Missing continuation metadata stays absent.
func crmPagination(op *v3.Operation, operation Operation) (*Pagination, error) {
	if operation.CrmOperation == "" || operation.Method != "GET" {
		return nil, nil
	}
	query := ""
	kind := ""
	if hasQueryParam(&operation, "cursor") {
		kind, query = "cursor", "cursor"
	} else if hasQueryParam(&operation, "page") {
		kind, query = "page", "page"
	} else {
		return nil, nil
	}
	if op.Responses == nil || op.Responses.Codes == nil {
		return nil, fmt.Errorf("%s: falta respuesta CRM paginada", operation.OperationID)
	}
	for pair := op.Responses.Codes.First(); pair != nil; pair = pair.Next() {
		if !strings.HasPrefix(pair.Key(), "2") {
			continue
		}
		response := pair.Value()
		if response == nil || response.Content == nil {
			continue
		}
		media, found := response.Content.Get("application/json")
		if !found || media == nil || media.Schema == nil {
			continue
		}
		if plan := findCRMPage(media.Schema.Schema(), nil, kind, 0); plan != nil {
			plan.QueryParameter = query
			return plan, nil
		}
	}
	return nil, nil
}

func findCRMPage(schema *base.Schema, path []string, kind string, depth int) *Pagination {
	if schema == nil || schema.Properties == nil || depth > 5 {
		return nil
	}
	property := func(name string) *base.Schema {
		proxy, found := schema.Properties.Get(name)
		if !found || proxy == nil {
			return nil
		}
		return proxy.Schema()
	}
	more, cursor := property("has_more"), property("next_cursor")
	if kind == "cursor" && more != nil && cursor != nil {
		for _, name := range []string{"data", "items", "results"} {
			item := property(name)
			if item != nil && schemaType(item, "array") && crmIdentity(item) != "" {
				return &Pagination{Kind: kind, ItemsPath: dotPath(path, name), MorePath: dotPath(path, "has_more"), CursorPath: dotPath(path, "next_cursor"), IdentityPath: crmIdentity(item)}
			}
		}
	}
	if kind == "page" {
		meta := property("meta")
		items := property("data")
		if meta != nil && meta.Properties != nil && items != nil && schemaType(items, "array") {
			current, currentFound := meta.Properties.Get("current_page")
			last, lastFound := meta.Properties.Get("last_page")
			if currentFound && lastFound && current != nil && last != nil && crmIdentity(items) != "" {
				return &Pagination{Kind: kind, ItemsPath: dotPath(path, "data"), CurrentPagePath: dotPath(path, "meta.current_page"), LastPagePath: dotPath(path, "meta.last_page"), IdentityPath: crmIdentity(items)}
			}
		}
	}
	// Only owner envelopes are descended. Nested related entities cannot become
	// the collection of a command accidentally.
	data := property("data")
	if data != nil && schemaType(data, "object") {
		return findCRMPage(data, append(append([]string{}, path...), "data"), kind, depth+1)
	}
	return nil
}

func dotPath(path []string, name string) string {
	return strings.Join(append(append([]string{}, path...), name), ".")
}
func schemaType(schema *base.Schema, expected string) bool {
	for _, item := range schema.Type {
		if item == expected {
			return true
		}
	}
	return false
}

func crmIdentity(array *base.Schema) string {
	if array.Items == nil || !array.Items.IsA() || array.Items.A == nil {
		return ""
	}
	item := array.Items.A.Schema()
	if item == nil {
		return ""
	}
	identity := func(schema *base.Schema) string {
		if schema == nil || schema.Properties == nil {
			return ""
		}
		for _, key := range []string{"id", "lead_id"} {
			if _, ok := schema.Properties.Get(key); ok {
				return key
			}
		}
		return ""
	}
	if key := identity(item); key != "" {
		return key
	}
	common := ""
	for _, variant := range item.OneOf {
		if variant == nil {
			return ""
		}
		key := identity(variant.Schema())
		if key == "" || (common != "" && common != key) {
			return ""
		}
		common = key
	}
	return common
}
